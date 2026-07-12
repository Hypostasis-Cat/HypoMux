import asyncio
import socket
import struct
import time
import threading
import psutil

# ==========================================
# 核心常量与网卡配置
# ==========================================
IP_UNICAST_IF = 31

NIC_ETH = {"name": "以太网", "index": 19, "ip": "10.20.236.208"}

# 极其重要：请在终端输入 ipconfig 确认 WLAN IPv4 地址还是不是 80！
NIC_WLAN = {"name": "WLAN", "index": 11, "ip": "192.168.31.80"} 

# ==========================================
# L4 连接调度器 (Balancer)
# ==========================================
class RoundRobinBalancer:
    def __init__(self):
        self.nics = [NIC_ETH, NIC_WLAN]
        self.current = 0
        self.lock = threading.Lock()

    def get_next_nic(self):
        with self.lock:
            nic = self.nics[self.current]
            self.current = (self.current + 1) % len(self.nics)
            return nic

balancer = RoundRobinBalancer()

# ==========================================
# L2 代理核心与 L3 绑定逻辑
# ==========================================
async def handle_client(reader, writer):
    try:
        # 1. SOCKS5 握手阶段
        version, nmethods = await reader.readexactly(2)
        methods = await reader.readexactly(nmethods)
        writer.write(b'\x05\x00')
        await writer.drain()

        # 2. 接收请求
        version, cmd, rsv, atyp = await reader.readexactly(4)
        if cmd != 1:
            writer.close()
            return

        dst_domain = None
        loop = asyncio.get_running_loop()

        if atyp == 1:  # IPv4
            dst_addr = socket.inet_ntoa(await reader.readexactly(4))
        elif atyp == 3:  # 域名
            domain_len = ord(await reader.readexactly(1))
            dst_domain = (await reader.readexactly(domain_len)).decode()
            try:
                # 【关键修复】前置异步解析 DNS，绕过单网卡解析死锁
                addr_info = await loop.getaddrinfo(dst_domain, None, family=socket.AF_INET, type=socket.SOCK_STREAM)
                dst_addr = addr_info[0][4][0]
            except Exception as e:
                print(f"[DNS失败] 无法解析域名 {dst_domain}: {e}")
                writer.close()
                return
        elif atyp == 4:
            writer.close()
            return
            
        dst_port = struct.unpack('!H', await reader.readexactly(2))[0]

        # 3. 【L4 调度】申请网卡
        nic = balancer.get_next_nic()
        target_display = dst_domain if dst_domain else dst_addr
        print(f"[调度分配] 新连接 -> 指派给: 【{nic['name']}】 | 目标: {target_display}:{dst_port}")

        # 4. 【L3 物理层绑定 -- 接口索引强绑定，根治 WinError 10049】
        #    先注入 IP_UNICAST_IF（IPPROTO_IP 级别，常量 31）把出口网卡锁死在
        #    nic['index'] 上，再 bind 源地址。同网段双网卡也不会再命中错网卡。
        upstream_sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        upstream_sock.setblocking(False)

        try:
            # IPv4 下接口索引必须用网络字节序（大端）
            upstream_sock.setsockopt(
                socket.IPPROTO_IP, IP_UNICAST_IF, struct.pack("!I", nic['index'])
            )
            try:
                upstream_sock.bind((nic['ip'], 0))
            except OSError:
                # 接口已由 IP_UNICAST_IF 锁定，bind 失败可降级忽略
                pass
        except Exception as e:
            print(f"[绑定崩溃] 网卡: {nic['name']} 接口强绑定 (IfIndex={nic['index']}) 失败: {e}。")
            writer.close()
            upstream_sock.close()
            return

        # 5. 连接目标
        try:
            await loop.sock_connect(upstream_sock, (dst_addr, dst_port))
        except Exception as e:
            print(f"[连通失败] 网卡: {nic['name']} 无法连接目标 {target_display}: {e}")
            writer.write(b'\x05\x05\x00\x01\x00\x00\x00\x00\x00\x00')
            await writer.drain()
            writer.close()
            upstream_sock.close()
            return

        # 连接成功
        writer.write(b'\x05\x00\x00\x01\x00\x00\x00\x00\x00\x00')
        await writer.drain()

        # 6. 数据透传
        _, pending = await asyncio.wait(
            [
                asyncio.create_task(relay(reader, upstream_sock, loop)),
                asyncio.create_task(relay_from_sock(upstream_sock, writer, loop))
            ],
            return_when=asyncio.FIRST_COMPLETED,
        )
        for task in pending:
            task.cancel()
            
    except Exception as e:
        pass
    finally:
        writer.close()

async def relay(reader, sock, loop):
    try:
        while True:
            data = await reader.read(65536)
            if not data:
                break
            await loop.sock_sendall(sock, data)
    except:
        pass
    finally:
        sock.close()

async def relay_from_sock(sock, writer, loop):
    try:
        while True:
            data = await loop.sock_recv(sock, 65536)
            if not data:
                break
            writer.write(data)
            await writer.drain()
    except:
        pass
    finally:
        sock.close()

async def start_proxy():
    server = await asyncio.start_server(handle_client, '127.0.0.1', 1080)
    async with server:
        await server.serve_forever()

# ==========================================
# 实时流量监控器
# ==========================================
def get_nic_bytes(io_counters, nic_name):
    if nic_name in io_counters:
        return io_counters[nic_name].bytes_recv
    return 0

def monitor_os_traffic():
    print("\n" + "="*70)
    print(f"{'时间':<10} | {'以太网 (Index 19) 真实下行':<25} | {'WLAN (Index 11) 真实下行':<25}")
    print("-" * 70)
    
    io_initial = psutil.net_io_counters(pernic=True)
    eth_last = get_nic_bytes(io_initial, NIC_ETH['name'])
    wlan_last = get_nic_bytes(io_initial, NIC_WLAN['name'])
    
    while True:
        time.sleep(1.0)
        io_now = psutil.net_io_counters(pernic=True)
        eth_now = get_nic_bytes(io_now, NIC_ETH['name'])
        wlan_now = get_nic_bytes(io_now, NIC_WLAN['name'])
        
        eth_speed = (eth_now - eth_last) / 1024 / 1024
        wlan_speed = (wlan_now - wlan_last) / 1024 / 1024
        
        print(f"{time.strftime('%H:%M:%S'):<10} | {eth_speed:>14.2f} MB/s            | {wlan_speed:>14.2f} MB/s")
        
        eth_last = eth_now
        wlan_last = wlan_now

if __name__ == "__main__":
    threading.Thread(target=monitor_os_traffic, daemon=True).start()
    print("[INFO] HypoMux SOCKS5 分发引擎启动中...")
    asyncio.run(start_proxy())