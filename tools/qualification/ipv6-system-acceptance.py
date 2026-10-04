import argparse
import ctypes
import datetime
import hashlib
import json
import pathlib
import queue
import subprocess
import sys
import threading
import time


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


def powershell(script):
    result = subprocess.run(
        ['powershell', '-NoProfile', '-NonInteractive', '-Command',
         '[Console]::OutputEncoding = New-Object System.Text.UTF8Encoding($false); ' + script],
        capture_output=True, encoding='utf-8', timeout=15,
        creationflags=subprocess.CREATE_NO_WINDOW,
    )
    require(result.returncode == 0, result.stderr)
    return json.loads(result.stdout or 'null')


def snapshot():
    return powershell(
        '$r=@(Get-NetRoute | Select-Object DestinationPrefix,NextHop,InterfaceIndex,InterfaceAlias,RouteMetric,Protocol); '
        '$a=@(Get-NetAdapter | Select-Object Name,Status,ifIndex); '
        '@{routes=$r;adapters=$a} | ConvertTo-Json -Depth 5 -Compress'
    )


def policy_routes(state):
    # Record all routes, but compare Teredo policy separately from its changing,
    # OS-generated local host routes. Every physical/foreign-VPN route is checked.
    def generated_teredo_host(route):
        return (route['InterfaceAlias'] == 'Teredo Tunneling Pseudo-Interface'
                and route['Protocol'] == 2 and route['DestinationPrefix'].endswith('/128'))
    return sorted(json.dumps(r, sort_keys=True) for r in state['routes']
                  if r['InterfaceAlias'] != 'HypoMux-Tun' and not generated_teredo_host(r))


def restored_snapshot(before):
    deadline = time.monotonic() + 20
    while True:
        current = snapshot()
        if policy_routes(before) == policy_routes(current) or time.monotonic() >= deadline:
            return current
        time.sleep(0.5)


def route_difference(before, after):
    return {'added': [r for r in after['routes'] if r not in before['routes']],
            'removed': [r for r in before['routes'] if r not in after['routes']]}


def require_restored(before, after):
    require(not any(a['Name'] == 'HypoMux-Tun' for a in after['adapters']), 'owned HypoMux-Tun remains')
    require(policy_routes(before) == policy_routes(after), 'foreign/physical or Teredo policy routes changed')
    for adapter in before['adapters']:
        if adapter['Status'] == 'Up':
            require(adapter in after['adapters'], 'an existing active adapter changed: ' + adapter['Name'])


class Core:
    def __init__(self, executable, output):
        self.stderr = (output / 'ipv6-system-core-stderr.txt').open('w', encoding='utf-8')
        self.process = subprocess.Popen(
            [executable, 'serve'], stdin=subprocess.PIPE, stdout=subprocess.PIPE,
            stderr=self.stderr, text=True, encoding='utf-8', creationflags=subprocess.CREATE_NO_WINDOW,
        )
        self.messages = queue.Queue()
        self.serial = 0
        threading.Thread(target=self.read_messages, daemon=True).start()

    def read_messages(self):
        for line in self.process.stdout:
            try:
                self.messages.put(json.loads(line))
            except ValueError:
                continue

    def call(self, method, params=None):
        self.serial += 1
        ident = str(self.serial)
        self.process.stdin.write(json.dumps({'protocol': 1, 'id': ident, 'method': method, 'params': params or {}}) + '\n')
        self.process.stdin.flush()
        deadline = time.monotonic() + 50
        while time.monotonic() < deadline:
            try:
                message = self.messages.get(timeout=max(0.1, deadline - time.monotonic()))
            except queue.Empty:
                break
            if message.get('id') == ident:
                if 'error' in message:
                    raise RuntimeError(str(message['error']))
                return message['result']
        raise RuntimeError('Core request timed out: ' + method)


def main():
    parser = argparse.ArgumentParser(description='Explicit Windows strict IPv6 TUN acceptance; run the PowerShell wrapper.')
    parser.add_argument('--input', type=pathlib.Path, required=True)
    args = parser.parse_args()
    prepared = json.loads(args.input.read_text(encoding='utf-8-sig'))
    output = args.input.resolve().parent
    adapter = prepared['adapter']
    report = {
        'tested_at_utc': datetime.datetime.now(datetime.timezone.utc).isoformat(),
        'windows_build': sys.getwindowsversion().build,
        'scope': 'elevated real Core, production strict IPv6 TUN, independent OS-routed client, normal and sidecar crash cleanup',
        'checks': [], 'adapter': adapter, 'full_acceptance': 'pending_network_matrix',
    }
    core = child = before = None
    started = False

    def record(name, evidence=None):
        report['checks'].append({'name': name, 'status': 'passed', 'evidence': evidence})

    try:
        require(ctypes.windll.shell32.IsUserAnAdmin(), 'system test requires an elevated controller')
        alias = adapter['name'].replace("'", "''")
        addresses = powershell("@(Get-NetIPAddress -InterfaceAlias '" + alias + "' -AddressFamily IPv6 | Select-Object IPAddress,AddressState) | ConvertTo-Json -Compress")
        if isinstance(addresses, dict):
            addresses = [addresses]
        require(any(a['IPAddress'] == adapter['source_ipv6'] and a['AddressState'] == 4 for a in addresses),
                'configured physical IPv6 source is no longer preferred')
        before = snapshot()
        report['before'] = before
        require(not any(a['Name'] == 'HypoMux-Tun' for a in before['adapters']), 'refuse to disturb an existing HypoMux TUN')
        report['core_sha256'] = hashlib.sha256(pathlib.Path(prepared['core']).read_bytes()).hexdigest()
        core = Core(prepared['core'], output)
        hello = core.call('engine.hello')
        require(hello['elevated'], 'Core is not elevated')
        record('elevated_real_core', hello)
        channels = [{'name': name, 'port': int(endpoint.rsplit(':', 1)[1]), 'adapter_names': [adapter['name']]}
                    for name, endpoint in prepared['endpoints'].items()]
        record('ipv6_only_pool_start', core.call('engine.start', {
            'mode': 'tun_tcp_pool', 'adapters': [adapter], 'dns': {'policy': 'alidns'}, 'channels': channels,
        }))
        target = None
        for name in ['www.qq.com', 'www.baidu.com']:
            try:
                answer = core.call('dns.resolve', {'domain': name, 'record_type': 'AAAA', 'adapter': adapter['name'], 'timeout_ms': 5000})
                target = (name, answer['address'])
                report['test_destination'] = answer
                break
            except Exception as error:
                report.setdefault('destination_errors', []).append({'domain': name, 'error': str(error)})
        require(target, 'no domestic IPv6 HTTPS destination resolved')
        activation = json.loads((pathlib.Path(prepared['output']) / 'activation.json').read_text(encoding='utf-8'))
        activation['executable'] = str(output / 'sing-box.exe')  # Exact bundled sibling required by the test Core.
        config_path = pathlib.Path(activation['config_path'])
        require(hashlib.sha256(config_path.read_bytes()).hexdigest() == activation['config_sha256'], 'configuration digest changed')
        state = core.call('tun.activate', activation)
        started = True
        require(not state.get('ipv4_only_fallback', False) and state['tun']['state'] == 'running', 'strict IPv6 activation failed or fell back')
        record('strict_ipv6_tun_activate', state)
        report['running'] = snapshot()
        require(any(a['Name'] == 'HypoMux-Tun' and a['Status'] == 'Up' for a in report['running']['adapters']), 'owned TUN is not active')
        child = subprocess.Popen(
            [str(output / 'ipv6-system-client.exe'), '-target=[' + target[1] + ']:443', '-server-name=' + target[0]],
            stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
            text=True, encoding='utf-8', creationflags=subprocess.CREATE_NO_WINDOW,
        )
        connected = json.loads(child.stdout.readline())
        require(connected['event'] == 'connected' and connected['certificate_verified'], 'independent client failed TLS verification')
        config = json.loads(config_path.read_text(encoding='utf-8'))
        tun6 = next(a.split('/')[0] for a in config['inbounds'][0]['address'] if ':' in a)
        require(connected['source'].startswith('[' + tun6 + ']:'), 'independent client bypassed the HypoMux IPv6 TUN')
        telemetry = core.call('engine.telemetry', {'include_connections': True})
        flows = [c for c in telemetry.get('active_connections', []) if c.get('adapter') == adapter['name']
                 and c.get('remote', '').startswith('[')
                 and (c.get('remote') == '[' + target[1] + ']:443' or target[0] in c.get('target', ''))]
        require(flows, 'independent system client did not traverse the selected IPv6 pool')
        record('independent_system_ipv6_tls_via_selected_pool', {'client': connected, 'flows': flows})
        child.stdin.write('fetch\n')
        child.stdin.flush()
        downloaded = json.loads(child.stdout.readline())
        child.wait(timeout=8)
        require(downloaded['event'] == 'downloaded' and child.returncode == 0, 'independent client payload verification failed')
        record('system_ipv6_https_payload', downloaded)
        child = None
        record('normal_tun_stop', core.call('tun.deactivate'))
        started = False
        after_stop = restored_snapshot(before)
        report['after_stop'] = after_stop
        report['normal_route_difference'] = route_difference(before, after_stop)
        require_restored(before, after_stop)
        record('normal_stop_routes_restored_and_other_tunnels_preserved')
        state = core.call('tun.activate', activation)
        started = True
        record('strict_tun_restart_for_owned_sidecar_crash', state)
        pid = state['tun']['pid']
        owner = powershell("Get-CimInstance Win32_Process -Filter 'ProcessId = " + str(pid) + "' | Select-Object ParentProcessId,ExecutablePath | ConvertTo-Json -Compress")
        require(owner and owner['ParentProcessId'] == core.process.pid and
                pathlib.Path(owner['ExecutablePath']).resolve() == (output / 'sing-box.exe').resolve(), 'sidecar ownership mismatch')
        subprocess.run(['taskkill', '/PID', str(pid), '/F'], capture_output=True, timeout=8,
                       creationflags=subprocess.CREATE_NO_WINDOW, check=True)
        deadline = time.monotonic() + 20
        while time.monotonic() < deadline:
            status = core.call('engine.status')
            if status.get('tun', {}).get('state') == 'failed':
                break
            time.sleep(0.2)
        require(status.get('tun', {}).get('state') == 'failed', 'owned sidecar crash was not reported as failed')
        record('cleanup_after_owned_sidecar_crash', core.call('tun.deactivate'))
        started = False
        after_crash = restored_snapshot(before)
        report['after_crash'] = after_crash
        report['crash_route_difference'] = route_difference(before, after_crash)
        require_restored(before, after_crash)
        record('sidecar_crash_routes_restored_and_other_tunnels_preserved', {'owned_sidecar_pid': pid})
        report['network_status'] = 'passed'
    except Exception as error:
        report.update(network_status='failed', error=str(error))
    finally:
        if child is not None:
            child.kill()
            child.wait(timeout=8)
            report['client_stderr'] = child.stderr.read()
        if core is not None:
            try:
                if started:
                    core.call('tun.deactivate')
                core.call('engine.stop')
                core.call('host.shutdown')
                core.process.wait(timeout=10)
            except Exception as error:
                report.update(network_status='failed', cleanup_error=str(error))
                core.process.terminate()
                core.process.wait(timeout=10)
            finally:
                core.stderr.close()
        if before is not None:
            try:
                report['final'] = restored_snapshot(before)
                require_restored(before, report['final'])
            except Exception as error:
                report.update(network_status='failed', final_restoration_error=str(error))
        (output / 'ipv6-system-tun-acceptance.json').write_text(json.dumps(report, ensure_ascii=False, indent=2) + '\n', encoding='utf-8')
        print(json.dumps({'network_status': report['network_status'], 'checks': len(report['checks']), 'error': report.get('error'), 'cleanup_error': report.get('cleanup_error')}))
    return 0 if report['network_status'] == 'passed' else 1


if __name__ == '__main__':
    sys.exit(main())
