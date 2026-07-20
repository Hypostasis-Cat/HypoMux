"""
HypoMux 网络工具模块 - Step 2

负责以下功能：
1. 管理员权限检测与 UAC 提权
2. 通过 PowerShell 扫描和解析网卡信息
3. 网卡跃点数（Metric）修改命令封装
4. 异常处理和错误日志

关键设计：
- 所有 PowerShell 命令都使用 InterfaceIndex（接口索引）而非中文别名
  避免中文编码导致的 subprocess 乱码问题
- 返回统一的数据结构（字典列表），方便 UI 绑定
- 完善的异常捕捉和超时处理机制
"""

import ctypes
from ctypes import wintypes
import sys
import socket
import subprocess
import json
import logging
from typing import List, Dict, Optional, Tuple


# ========== 日志配置 ==========
logger = logging.getLogger(__name__)
logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s - %(name)s - %(levelname)s - %(message)s'
)


def _get_windows_startupinfo():
    """
    获取 Windows 下用于隐藏子进程窗口的 STARTUPINFO。
    """
    startupinfo = None
    if sys.platform == "win32":
        startupinfo = subprocess.STARTUPINFO()
        startupinfo.dwFlags |= subprocess.STARTF_USESHOWWINDOW
        startupinfo.wShowWindow = 0
    return startupinfo


# ========== 网卡接口索引（IfIndex）权威读取 ==========
# 通过 Win32 IPHLPAPI 的 GetAdaptersAddresses 直接拿到每张活动网卡的
# IfIndex（接口索引）与其上挂载的所有单播 IPv4 地址。
#
# 这是修复 WinError 10049（在同网段网卡上 bind 本地 IP 会随机命中错误网卡）的
# 物理地基：拿到 IfIndex 后，上层即可用 IP_UNICAST_IF 把出站 socket 死锁在
# 指定网卡上，彻底绕过 Windows 的默认路由查找，而不再依赖脆弱的 bind(local_ip)。

# GetAdaptersAddresses 常量
_AF_INET = 2                     # AF_INET
_AF_UNSPEC = 0
_GAA_FLAG_SKIP_ANYCAST = 0x0002
_GAA_FLAG_SKIP_MULTICAST = 0x0004
_GAA_FLAG_SKIP_DNS_SERVER = 0x0008
_ERROR_BUFFER_OVERFLOW = 111
_ERROR_SUCCESS = 0
_IF_OPER_STATUS_UP = 1           # IfOperStatusUp

# ===== 接口类型 (IfType, 来自 IANA ifType / Win32 IPIFCONS.H) =====
# 任务1 核心：拨号上网 (PPPoE「宽带连接」) 会生成 WAN Miniport PPP 虚拟接口，
# 其 IfType == 23 (IF_TYPE_PPP)。旧逻辑只认物理以太网/WLAN，导致 PPP 链路被
# 丢弃、上层强行 bind 被架空的物理网卡 IP → WinError 1231。
# 这里把 PPP 显式纳入白名单，并排除回环 / 隧道等无意义出口。
_IF_TYPE_ETHERNET_CSMACD = 6     # 物理以太网
_IF_TYPE_PPP = 23                # 拨号 PPP / PPPoE 宽带连接虚拟网卡
_IF_TYPE_SOFTWARE_LOOPBACK = 24  # 回环（排除）
_IF_TYPE_IEEE80211 = 71          # 无线 WLAN
_IF_TYPE_TUNNEL = 131            # 隧道（排除，避免 Teredo/ISATAP 干扰）

# 允许进入加速池的接口类型白名单：物理以太网 / WLAN / PPP 拨号
_ALLOWED_IF_TYPES = {
    _IF_TYPE_ETHERNET_CSMACD,
    _IF_TYPE_PPP,
    _IF_TYPE_IEEE80211,
}
# 明确排除的接口类型（即便 UP 也不作为出站链路）
_EXCLUDED_IF_TYPES = {
    _IF_TYPE_SOFTWARE_LOOPBACK,
    _IF_TYPE_TUNNEL,
}


class _SOCKADDR(ctypes.Structure):
    _fields_ = [
        ("sa_family", wintypes.USHORT),
        ("sa_data", ctypes.c_ubyte * 14),
    ]


class _SOCKET_ADDRESS(ctypes.Structure):
    _fields_ = [
        ("lpSockaddr", ctypes.POINTER(_SOCKADDR)),
        ("iSockaddrLength", ctypes.c_int),
    ]


class _IP_ADAPTER_UNICAST_ADDRESS(ctypes.Structure):
    pass


class _IP_ADAPTER_DNS_SERVER_ADDRESS(ctypes.Structure):
    pass


_IP_ADAPTER_UNICAST_ADDRESS._fields_ = [
    ("Length", wintypes.ULONG),
    ("Flags", wintypes.DWORD),
    ("Next", ctypes.POINTER(_IP_ADAPTER_UNICAST_ADDRESS)),
    ("Address", _SOCKET_ADDRESS),
    ("PrefixOrigin", ctypes.c_int),
    ("SuffixOrigin", ctypes.c_int),
    ("DadState", ctypes.c_int),
    ("ValidLifetime", wintypes.ULONG),
    ("PreferredLifetime", wintypes.ULONG),
    ("LeaseLifetime", wintypes.ULONG),
    ("OnLinkPrefixLength", ctypes.c_ubyte),
]


_IP_ADAPTER_DNS_SERVER_ADDRESS._fields_ = [
    ("Length", wintypes.ULONG),
    ("Reserved", wintypes.DWORD),
    ("Next", ctypes.POINTER(_IP_ADAPTER_DNS_SERVER_ADDRESS)),
    ("Address", _SOCKET_ADDRESS),
]


class _IP_ADAPTER_ADDRESSES(ctypes.Structure):
    pass


_IP_ADAPTER_ADDRESSES._fields_ = [
    ("Length", wintypes.ULONG),
    ("IfIndex", wintypes.DWORD),
    ("Next", ctypes.POINTER(_IP_ADAPTER_ADDRESSES)),
    ("AdapterName", ctypes.c_char_p),
    ("FirstUnicastAddress", ctypes.POINTER(_IP_ADAPTER_UNICAST_ADDRESS)),
    ("FirstAnycastAddress", ctypes.c_void_p),
    ("FirstMulticastAddress", ctypes.c_void_p),
    ("FirstDnsServerAddress", ctypes.c_void_p),
    ("DnsSuffix", wintypes.LPWSTR),
    ("Description", wintypes.LPWSTR),
    ("FriendlyName", wintypes.LPWSTR),
    ("PhysicalAddress", ctypes.c_ubyte * 8),
    ("PhysicalAddressLength", wintypes.DWORD),
    ("Flags", wintypes.DWORD),
    ("Mtu", wintypes.DWORD),
    ("IfType", wintypes.DWORD),
    ("OperStatus", ctypes.c_int),
    ("Ipv6IfIndex", wintypes.DWORD),
    ("ZoneIndices", wintypes.DWORD * 16),
    ("FirstPrefix", ctypes.c_void_p),
    # 以下字段在新版结构体中存在，这里无需逐一访问，但保留以保证 Length 对齐
    ("TransmitLinkSpeed", ctypes.c_uint64),
    ("ReceiveLinkSpeed", ctypes.c_uint64),
    ("FirstWinsServerAddress", ctypes.c_void_p),
    ("FirstGatewayAddress", ctypes.c_void_p),
    ("Ipv4Metric", wintypes.ULONG),
    ("Ipv6Metric", wintypes.ULONG),
]


def _sockaddr_to_ipv4(socket_address: _SOCKET_ADDRESS) -> Optional[str]:
    """从 SOCKET_ADDRESS 中提取点分十进制 IPv4 字符串（仅处理 AF_INET）。"""
    sockaddr_ptr = socket_address.lpSockaddr
    if not sockaddr_ptr:
        return None
    sockaddr = sockaddr_ptr.contents
    if sockaddr.sa_family != _AF_INET:
        return None
    # sockaddr_in: family(2) + port(2) + in_addr(4) ...
    # sa_data[2:6] 即为 4 字节 IPv4 地址（网络字节序）
    raw = bytes(sockaddr.sa_data[2:6])
    return socket.inet_ntoa(raw)


def _adapter_dns_servers_ipv4(adapter: _IP_ADAPTER_ADDRESSES) -> List[str]:
    """读取网卡自身的 IPv4 DNS 服务器列表。"""
    servers: List[str] = []
    dns_ptr = None
    if adapter.FirstDnsServerAddress:
        dns_ptr = ctypes.cast(
            adapter.FirstDnsServerAddress,
            ctypes.POINTER(_IP_ADAPTER_DNS_SERVER_ADDRESS),
        )
    while dns_ptr:
        dns = _sockaddr_to_ipv4(dns_ptr.contents.Address)
        if dns and dns not in servers:
            servers.append(dns)
        dns_ptr = dns_ptr.contents.Next
    return servers


def _adapter_gateways_ipv4(adapter: _IP_ADAPTER_ADDRESSES) -> List[str]:
    """读取网卡的 IPv4 默认网关（原生回退扫描也可用，无需 CIM 权限）。"""
    gateways: List[str] = []
    gateway_ptr = None
    if adapter.FirstGatewayAddress:
        # IP_ADAPTER_GATEWAY_ADDRESS_LH 与 DNS 地址节点的内存布局相同：
        # Length / Reserved / Next / SOCKET_ADDRESS。
        gateway_ptr = ctypes.cast(
            adapter.FirstGatewayAddress,
            ctypes.POINTER(_IP_ADAPTER_DNS_SERVER_ADDRESS),
        )
    while gateway_ptr:
        gateway = _sockaddr_to_ipv4(gateway_ptr.contents.Address)
        if gateway and gateway not in gateways:
            gateways.append(gateway)
        gateway_ptr = gateway_ptr.contents.Next
    return gateways


def get_adapter_if_indices() -> Dict[str, int]:
    """
    调用 GetAdaptersAddresses，返回 {IPv4 地址: IfIndex} 的映射。

    上层在创建出站 socket 时，可用网卡的出口 IP 反查其真实 IfIndex，再用
    IP_UNICAST_IF 把 socket 锁死在该网卡上。这样即使两张网卡处于同一网段，
    也不会再发生 bind(local_ip) 命中错网卡导致的 WinError 10049。

    Returns:
        Dict[str, int]: 形如 {"192.168.31.80": 11, "10.20.236.208": 19}
        失败时返回空字典（上层应有回退逻辑）。
    """
    ip_to_index: Dict[str, int] = {}
    try:
        iphlpapi = ctypes.windll.Iphlpapi
        flags = _GAA_FLAG_SKIP_ANYCAST | _GAA_FLAG_SKIP_MULTICAST

        buf_len = wintypes.ULONG(15 * 1024)  # 初始 15KB，按官方建议预分配
        for _ in range(3):  # 缓冲区不足时按返回的 SizePointer 重试
            buffer = ctypes.create_string_buffer(buf_len.value)
            ret = iphlpapi.GetAdaptersAddresses(
                _AF_INET,
                flags,
                None,
                ctypes.cast(buffer, ctypes.POINTER(_IP_ADAPTER_ADDRESSES)),
                ctypes.byref(buf_len),
            )
            if ret == _ERROR_BUFFER_OVERFLOW:
                continue  # buf_len 已被写为所需大小，重试
            break

        if ret != _ERROR_SUCCESS:
            logger.warning(f"GetAdaptersAddresses 返回错误码 {ret}")
            return ip_to_index

        adapter_ptr = ctypes.cast(buffer, ctypes.POINTER(_IP_ADAPTER_ADDRESSES))
        while adapter_ptr:
            adapter = adapter_ptr.contents
            # 只采集处于 Up 状态的网卡
            if adapter.OperStatus == _IF_OPER_STATUS_UP and adapter.IfIndex != 0:
                unicast_ptr = adapter.FirstUnicastAddress
                while unicast_ptr:
                    unicast = unicast_ptr.contents
                    ipv4 = _sockaddr_to_ipv4(unicast.Address)
                    if ipv4:
                        ip_to_index[ipv4] = int(adapter.IfIndex)
                    unicast_ptr = unicast.Next
            adapter_ptr = adapter.Next

        logger.info(f"GetAdaptersAddresses 解析出 {len(ip_to_index)} 个 IPv4->IfIndex 映射")
    except Exception as e:
        logger.error(f"GetAdaptersAddresses 调用异常: {type(e).__name__}: {e}")

    return ip_to_index


def resolve_if_index(ipv4: str, fallback: Optional[int] = None) -> Optional[int]:
    """
    用出口 IPv4 地址反查其网卡的 IfIndex（接口索引）。

    Args:
        ipv4: 网卡的出口 IPv4 地址
        fallback: 查不到时返回的回退值（通常是 UI 扫描阶段记录的 index）

    Returns:
        匹配到的 IfIndex；查不到时返回 fallback。
    """
    if not ipv4:
        return fallback
    mapping = get_adapter_if_indices()
    return mapping.get(ipv4, fallback)


def _is_routable_ipv4(ip: str) -> bool:
    """判断一个 IPv4 是否可作为有效出站源（排除回环 / 链路本地 / 全零）。

    - 127.0.0.0/8       回环
    - 169.254.0.0/16    APIPA 链路本地（拿不到 DHCP 时的兜底地址，不可路由）
    - 0.0.0.0           未配置
    PPPoE 拨号分配到的动态公网/运营商 IP 会顺利通过本校验。
    """
    if not ip:
        return False
    parts = ip.split(".")
    if len(parts) != 4 or not all(p.isdigit() for p in parts):
        return False
    try:
        a, b = int(parts[0]), int(parts[1])
    except ValueError:
        return False
    if a == 127:                       # 回环
        return False
    if a == 169 and b == 254:          # APIPA 链路本地
        return False
    if a == 0:                         # 未配置
        return False
    return True


def get_adapter_full_info() -> List[Dict]:
    """
    调用 GetAdaptersAddresses，原生枚举所有 UP 网卡（含 PPP 拨号虚拟网卡）。

    【任务1 核心】GetAdaptersAddresses 是 Win32 IPHLPAPI 的权威枚举接口，
    它能直接列出 WAN Miniport PPP（IfType=23）拨号网卡，并给出其分配到的
    真实动态 IP 与 IfIndex —— 而 Get-NetAdapter 往往看不到 PPP 接口，这正是
    旧扫描漏掉「宽带连接」、上层误绑物理网卡 IP 触发 WinError 1231 的根因。

    Returns:
        List[Dict]: 每项 {index, iftype, friendly, ipv4_list}
                    仅包含 OperStatus=Up、且接口类型不在排除集合中的网卡。
                    失败时返回空列表（上层应有回退逻辑）。
    """
    results: List[Dict] = []
    try:
        iphlpapi = ctypes.windll.Iphlpapi
        flags = _GAA_FLAG_SKIP_ANYCAST | _GAA_FLAG_SKIP_MULTICAST

        buf_len = wintypes.ULONG(15 * 1024)
        ret = _ERROR_BUFFER_OVERFLOW
        buffer = None
        for _ in range(3):
            buffer = ctypes.create_string_buffer(buf_len.value)
            ret = iphlpapi.GetAdaptersAddresses(
                _AF_INET, flags, None,
                ctypes.cast(buffer, ctypes.POINTER(_IP_ADAPTER_ADDRESSES)),
                ctypes.byref(buf_len),
            )
            if ret == _ERROR_BUFFER_OVERFLOW:
                continue
            break

        if ret != _ERROR_SUCCESS or buffer is None:
            logger.warning(f"get_adapter_full_info: GetAdaptersAddresses 返回 {ret}")
            return results

        adapter_ptr = ctypes.cast(buffer, ctypes.POINTER(_IP_ADAPTER_ADDRESSES))
        while adapter_ptr:
            adapter = adapter_ptr.contents
            iftype = int(adapter.IfType)
            oper_up = (adapter.OperStatus == _IF_OPER_STATUS_UP)

            # 排除回环 / 隧道；仅保留 UP 且 IfIndex 有效的网卡
            if oper_up and adapter.IfIndex != 0 and iftype not in _EXCLUDED_IF_TYPES:
                ipv4_list: List[str] = []
                dns_servers = _adapter_dns_servers_ipv4(adapter)
                gateways = _adapter_gateways_ipv4(adapter)
                unicast_ptr = adapter.FirstUnicastAddress
                while unicast_ptr:
                    unicast = unicast_ptr.contents
                    ipv4 = _sockaddr_to_ipv4(unicast.Address)
                    if ipv4 and _is_routable_ipv4(ipv4):
                        ipv4_list.append(ipv4)
                    unicast_ptr = unicast.Next

                if ipv4_list:
                    friendly = adapter.FriendlyName or ""
                    results.append({
                        "index": int(adapter.IfIndex),
                        "iftype": iftype,
                        "is_ppp": iftype == _IF_TYPE_PPP,
                        "friendly": friendly,
                        "ipv4_list": ipv4_list,
                        "dns_servers": dns_servers,
                        "gateways": gateways,
                        "metric": int(adapter.Ipv4Metric),
                    })
            adapter_ptr = adapter.Next

        logger.info(f"get_adapter_full_info: 枚举到 {len(results)} 个可用网卡（含 PPP={sum(1 for r in results if r['is_ppp'])}）")
    except Exception as e:
        logger.error(f"get_adapter_full_info 调用异常: {type(e).__name__}: {e}")

    return results


# ========== 管理员权限检测与提权 ==========

def is_admin() -> bool:
    """
    检测程序是否以管理员身份运行
    
    Returns:
        bool: True 表示有管理员权限，False 表示无权限
    """
    try:
        return ctypes.windll.shell32.IsUserAnAdmin()
    except Exception as e:
        logger.error(f"检测管理员权限时出错: {e}")
        return False


def elevate_privileges() -> bool:
    """
    如果程序没有管理员权限，使用 UAC 提权重启程序
    
    Returns:
        bool: True 表示成功提权并重启，False 表示已是管理员或提权失败
    """
    if is_admin():
        logger.info("程序已以管理员身份运行")
        return False
    
    try:
        # 获取当前 Python 脚本的完整路径
        script_path = sys.argv[0]
        
        # 使用 ShellExecuteW 重新启动程序，runas 参数会触发 UAC 提权
        ctypes.windll.shell32.ShellExecuteW(
            None,                    # hwnd
            "runas",                 # op (操作：以管理员身份运行)
            sys.executable,          # lpFile (Python 可执行文件)
            script_path,             # lpParameters (脚本路径作为参数)
            None,                    # lpDirectory
            1                        # nShowCmd (SW_NORMAL)
        )
        
        logger.info("已发起 UAC 提权请求，程序将重新启动...")
        return True
    except Exception as e:
        logger.error(f"UAC 提权失败: {e}")
        return False


# ========== 网卡信息扫描与解析 ==========

def _run_powershell_command(command: str, timeout: int = 10) -> Tuple[bool, str]:
    """
    执行 PowerShell 命令并返回结果

    Args:
        command: 要执行的 PowerShell 命令
        timeout: 执行超时时间（秒）

    Returns:
        Tuple[bool, str]: (是否成功, 输出内容或错误信息)
    """
    try:
        startupinfo = _get_windows_startupinfo()
        utf8_command = "[Console]::OutputEncoding=[System.Text.Encoding]::UTF8; " + command
        result = subprocess.run(
            ["powershell", "-NoProfile", "-Command", utf8_command],
            capture_output=True,
            text=True,
            timeout=timeout,
            startupinfo=startupinfo,
            encoding="utf-8",
            errors="replace",
        )

        stdout = (result.stdout or "").strip()
        stderr = (result.stderr or "").strip()

        if result.returncode == 0:
            return True, stdout
        else:
            error_msg = stderr or stdout
            logger.warning(f"PowerShell 命令执行返回非零状态码 {result.returncode}: {error_msg}")
            return False, error_msg

    except subprocess.TimeoutExpired:
        error_msg = f"PowerShell 命令执行超时（{timeout}s）"
        logger.error(error_msg)
        return False, error_msg

    except Exception as e:
        error_msg = f"执行 PowerShell 命令出错: {type(e).__name__}: {str(e)}"
        logger.error(error_msg)
        return False, error_msg


def _parse_adapter_from_json(adapter_json: Dict) -> Optional[Dict]:
    """
    从 JSON 对象解析网卡信息
    
    Args:
        adapter_json: 从 PowerShell 返回的 JSON 对象
    
    Returns:
        Dict 或 None: 解析后的网卡信息，失败返回 None
    """
    try:
        iftype = int(adapter_json.get("IfType") or -1)
        alias = adapter_json.get("InterfaceAlias", "Unknown")
        # PPP 判定：IfType=23，或别名含拨号/宽带特征词
        is_ppp = (iftype == _IF_TYPE_PPP) or any(
            kw in str(alias) for kw in ("PPP", "宽带", "Broadband", "Dial")
        )
        adapter_info = {
            "index": int(adapter_json.get("InterfaceIndex", -1)),
            "alias": alias,
            "ipv4": adapter_json.get("IPv4Address", "N/A"),
            "dns_servers": _normalize_dns_servers(adapter_json.get("DNSServers")),
            "is_auto": adapter_json.get("AutomaticMetric", True),
            "metric": int(adapter_json.get("InterfaceMetric") or -1),
            "iftype": iftype,
            "is_ppp": is_ppp,
            "gateway": str(adapter_json.get("DefaultGateway") or "").strip(),
            "connection_state": str(adapter_json.get("ConnectionState") or "").strip(),
        }
        return adapter_info
    except Exception as e:
        logger.warning(f"解析网卡信息出错: {e}")
        return None


def _normalize_dns_servers(raw) -> List[str]:
    """规整 PowerShell / 原生枚举返回的 DNS 服务器字段。"""
    values: List[str] = []
    if isinstance(raw, list):
        candidates = raw
    elif raw:
        candidates = str(raw).replace(";", ",").split(",")
    else:
        candidates = []
    for item in candidates:
        text = str(item).strip()
        parts = text.split(".")
        if len(parts) == 4 and all(p.isdigit() and 0 <= int(p) <= 255 for p in parts):
            if text not in values:
                values.append(text)
    return values


def scan_network_adapters() -> Tuple[bool, List[Dict], str]:
    """
    扫描系统中所有已连接且拥有 IPv4 地址的网卡（含 PPPoE 拨号虚拟网卡）。

    【任务1 修复】采用「双引擎合并」策略根治拨号上网无法加速的 Bug：
    1. PowerShell (Get-NetIPInterface) 主引擎：覆盖物理以太网/WLAN。
    2. GetAdaptersAddresses 原生引擎 (get_adapter_full_info) 补全：
       专门捞回 PowerShell 常常遗漏的 WAN Miniport PPP (IfType=23) 拨号网卡，
       并以其【真实分配到的动态 IP】入池，避免误绑被架空的物理网卡 IP。

    过滤条件：
    - 接口处于 Up / Connected
    - 接口类型属于白名单（以太网 / WLAN / PPP），排除回环、隧道
    - 拥有有效可路由 IPv4（排除 127.x / 169.254.x / 0.x）

    Returns:
        Tuple[bool, List[Dict], str]:
            - 是否成功获取
            - 网卡信息列表（每项含 index/alias/ipv4/is_auto/metric/iftype/is_ppp）
            - 错误信息或空字符串
    """
    # PowerShell 命令：抓取 IPv4 接口。新增 ifType 字段，并放宽 PPP 接口
    # （PPP 接口的 Get-NetAdapter Status 常为非 'Up'，故对 PPP 单独放行）。
    ps_command = """
    $adapters = @()
    $interfaces = Get-NetIPInterface -AddressFamily IPv4 | Where-Object { $_.ConnectionState -eq 'Connected' }

    foreach ($interface in $interfaces) {
        $ifIndex = $interface.InterfaceIndex
        $ifAlias = $interface.InterfaceAlias
        $autoMetric = $interface.AutomaticMetric
        $ifMetric = $interface.InterfaceMetric
        $netConfig = Get-NetIPConfiguration -InterfaceIndex $ifIndex -ErrorAction SilentlyContinue
        $gateway = @(
            $netConfig.IPv4DefaultGateway |
            Select-Object -ExpandProperty NextHop -ErrorAction SilentlyContinue
        ) -join ', '

        $ipv4Addr = (Get-NetIPAddress -InterfaceIndex $ifIndex -AddressFamily IPv4 -ErrorAction SilentlyContinue).IPAddress
        $dnsServers = @(
            Get-DnsClientServerAddress -InterfaceIndex $ifIndex -AddressFamily IPv4 -ErrorAction SilentlyContinue |
            Select-Object -ExpandProperty ServerAddresses -ErrorAction SilentlyContinue |
            Where-Object { $_ -match '^\\d{1,3}(\\.\\d{1,3}){3}$' }
        )

        $adapter = Get-NetAdapter -InterfaceIndex $ifIndex -ErrorAction SilentlyContinue
        $status = $adapter.Status
        $ifType = $adapter.ifType

        # PPP 拨号接口 (ifType=23) 即便 Get-NetAdapter 状态非 Up 也放行，
        # 只要它拿到了有效 IPv4，就是一条真实可用的宽带拨号链路。
        $isPpp = ($ifType -eq 23) -or ($ifAlias -match 'PPP' -or $ifAlias -match '宽带' -or $ifAlias -match 'Broadband' -or $ifAlias -match 'Dial')

        if ($ipv4Addr -and (($status -eq 'Up') -or $isPpp)) {
            $adapters += @{
                InterfaceIndex = $ifIndex
                InterfaceAlias = $ifAlias
                IPv4Address = $ipv4Addr
                DNSServers = $dnsServers
                AutomaticMetric = $autoMetric
                InterfaceMetric = $ifMetric
                Status = $status
                ConnectionState = $interface.ConnectionState
                DefaultGateway = $gateway
                IfType = $ifType
            }
        }
    }

    $adapters | ConvertTo-Json -Depth 2
    """

    try:
        ps_success, ps_output = _run_powershell_command(ps_command, timeout=15)

        adapters: List[Dict] = []
        seen_indices = set()

        if ps_success and ps_output and ps_output.lower() != "null":
            try:
                adapters_json = json.loads(ps_output)
                if isinstance(adapters_json, dict):
                    adapters_json = [adapters_json]
                if isinstance(adapters_json, list):
                    for adapter_json in adapters_json:
                        adapter_info = _parse_adapter_from_json(adapter_json)
                        if adapter_info and adapter_info["index"] not in seen_indices:
                            adapters.append(adapter_info)
                            seen_indices.add(adapter_info["index"])
                            logger.info(
                                f"[PS] 网卡: {adapter_info['alias']} "
                                f"(Index: {adapter_info['index']}, IPv4: {adapter_info['ipv4']}, "
                                f"PPP: {adapter_info.get('is_ppp')})"
                            )
            except json.JSONDecodeError as e:
                logger.warning(f"PowerShell 输出 JSON 解析失败，将仅依赖原生枚举: {e}")
        else:
            logger.warning(f"PowerShell 扫描未返回有效数据，将依赖原生枚举补全: {ps_output[:120] if ps_output else ''}")

        # ===== 原生 GetAdaptersAddresses 补全 PPP 等被 PS 漏掉的网卡 =====
        try:
            native = get_adapter_full_info()
            for n in native:
                if n["index"] in seen_indices:
                    continue
                # 取第一个可路由 IPv4 作为该网卡出口
                ipv4 = n["ipv4_list"][0] if n["ipv4_list"] else ""
                if not ipv4:
                    continue
                alias = n["friendly"] or f"Adapter-{n['index']}"
                adapters.append({
                    "index": n["index"],
                    "alias": alias,
                    "ipv4": ", ".join(n["ipv4_list"]),
                    "dns_servers": n.get("dns_servers", []),
                    "is_auto": True,
                    "metric": n.get("metric", -1),
                    "iftype": n["iftype"],
                    "is_ppp": n["is_ppp"],
                    "gateway": ", ".join(n.get("gateways", [])),
                    "connection_state": "Connected",
                })
                seen_indices.add(n["index"])
                logger.info(
                    f"[Native] 补全网卡: {alias} (Index: {n['index']}, IPv4: {ipv4}, PPP: {n['is_ppp']})"
                )
        except Exception as e:
            logger.warning(f"原生枚举补全失败（不影响已扫描结果）: {e}")

        if not ps_success and not adapters:
            error_msg = f"PowerShell 执行失败且原生枚举无结果: {ps_output}"
            logger.error(error_msg)
            return False, [], error_msg

        logger.info(f"共发现 {len(adapters)} 个可用网卡（含 PPP）")
        return True, adapters, ""

    except Exception as e:
        error_msg = f"扫描网卡时发生异常: {type(e).__name__}: {str(e)}"
        logger.error(error_msg)
        return False, [], error_msg


# ========== 跃点数修改 ==========

def set_adapter_metric(
    if_index: int,
    metric: Optional[int] = None,
    auto_metric: bool = True
) -> Tuple[bool, str]:
    """
    修改指定网卡的跃点数（Metric）
    
    Args:
        if_index: 网卡接口索引（InterfaceIndex）
        metric: 要设置的跃点数（1-9999）。当 auto_metric=True 时可为 None
        auto_metric: 是否启用自动跃点模式。True 时自动分配，False 时使用固定值
    
    Returns:
        Tuple[bool, str]: (是否成功, 执行结果或错误信息)
    
    示例：
        # 设置接口 12 的跃点数为 10（禁用自动）
        set_adapter_metric(if_index=12, metric=10, auto_metric=False)
        
        # 恢复接口 12 为自动跃点
        set_adapter_metric(if_index=12, auto_metric=True)
    """
    try:
        # 验证输入
        if not isinstance(if_index, int) or if_index <= 0:
            error_msg = f"无效的接口索引: {if_index}"
            logger.error(error_msg)
            return False, error_msg
        
        if not auto_metric:
            if metric is None or not isinstance(metric, int) or metric < 1 or metric > 9999:
                error_msg = f"当禁用自动跃点时，跃点数必须为 1-9999 之间的整数，得到: {metric}"
                logger.error(error_msg)
                return False, error_msg
        
        # 构建 PowerShell 命令
        if auto_metric:
            # 恢复为自动跃点
            ps_command = f"Set-NetIPInterface -InterfaceIndex {if_index} -AutomaticMetric Enabled"
            log_action = f"恢复接口 {if_index} 为自动跃点模式"
        else:
            # 设置为固定跃点
            ps_command = f"Set-NetIPInterface -InterfaceIndex {if_index} -AutomaticMetric Disabled -InterfaceMetric {metric}"
            log_action = f"设置接口 {if_index} 的跃点数为 {metric}"
        
        logger.info(f"执行操作: {log_action}")
        
        # 执行命令
        ps_success, ps_output = _run_powershell_command(ps_command, timeout=10)
        
        if ps_success:
            success_msg = f"成功: {log_action}"
            logger.info(success_msg)
            return True, success_msg
        else:
            # 如果执行失败，检查是否因权限问题
            if "access is denied" in ps_output.lower() or "权限" in ps_output:
                error_msg = f"权限不足（需要管理员权限）: {ps_output}"
            else:
                error_msg = f"执行失败: {ps_output}"
            logger.error(error_msg)
            return False, error_msg
    
    except Exception as e:
        error_msg = f"修改网卡跃点数时发生异常: {type(e).__name__}: {str(e)}"
        logger.error(error_msg)
        return False, error_msg


def batch_set_adapter_metrics(
    adapter_metrics: List[Tuple[int, int, bool]]
) -> Tuple[bool, List[Dict]]:
    """
    批量修改多个网卡的跃点数
    
    Args:
        adapter_metrics: 列表，每项为 (接口索引, 跃点数, 是否自动)
                        例如: [(12, 10, False), (8, None, True)]
    
    Returns:
        Tuple[bool, List[Dict]]: 
            - 第1个元素：是否全部成功
            - 第2个元素：结果列表，每项包含 {if_index, success, message}
    """
    results = []
    all_success = True
    
    for if_index, metric, auto_metric in adapter_metrics:
        success, message = set_adapter_metric(if_index, metric, auto_metric)
        results.append({
            "if_index": if_index,
            "success": success,
            "message": message
        })
        if not success:
            all_success = False
    
    return all_success, results


# ========== 便利函数 ==========

def boost_adapter_metric(if_index: int) -> Tuple[bool, str]:
    """
    一键加速：将网卡跃点数设为 10（用于提升网卡优先级）
    
    Args:
        if_index: 网卡接口索引
    
    Returns:
        Tuple[bool, str]: (是否成功, 结果信息)
    """
    return set_adapter_metric(if_index, metric=10, auto_metric=False)


def reset_adapter_metric(if_index: int) -> Tuple[bool, str]:
    """
    恢复网卡到自动跃点模式（游戏模式 - 恢复正常路由）
    
    Args:
        if_index: 网卡接口索引
    
    Returns:
        Tuple[bool, str]: (是否成功, 结果信息)
    """
    return set_adapter_metric(if_index, auto_metric=True)


# ========== 全局 TCP/系统底层策略：死网关检测 ==========

def set_dead_gateway_detection(enabled: bool) -> Tuple[bool, str]:
    """
    开关 Windows 的死网关检测（Dead Gateway Detection）机制。

    多网卡并发下载时，慢速链路被瞬间塞爆会触发 Windows TCP/IP 状态机的
    死网关检测，导致慢网卡被系统判定为"失效"而中途罢工。关闭该机制可
    阻止系统主动放弃慢速链路，从而维持多网卡并发的稳定性。

    - 加速启动时：禁用（disabled），阻止系统踢掉慢网卡
    - 恢复默认/退出时：启用（enabled），还原系统默认行为

    使用纯数字/英文参数，不涉及任何中文字符，杜绝乱码隐患。

    Args:
        enabled: True 恢复系统默认（enabled），False 关闭检测（disabled）

    Returns:
        Tuple[bool, str]: (是否成功, 结果信息)
    """
    state = "enabled" if enabled else "disabled"
    ps_command = f"netsh interface ipv4 set global deadgatewaydetection={state}"
    log_action = f"{'恢复' if enabled else '关闭'}死网关检测 (deadgatewaydetection={state})"

    try:
        logger.info(f"执行操作: {log_action}")
        ps_success, ps_output = _run_powershell_command(ps_command, timeout=10)

        if ps_success:
            success_msg = f"成功: {log_action}"
            logger.info(success_msg)
            return True, success_msg
        else:
            error_msg = f"执行失败: {ps_output}"
            logger.error(error_msg)
            return False, error_msg
    except Exception as e:
        error_msg = f"设置死网关检测时发生异常: {type(e).__name__}: {str(e)}"
        logger.error(error_msg)
        return False, error_msg


# ========== 动态路由微调防僵死机制（Metric Jiggling） ==========

def jiggle_adapter_metric(
    if_index: int,
    base_metric: int = 10,
    jiggle_delay_ms: int = 5
) -> Tuple[bool, str]:
    """
    对单个网卡执行一次无感的"跃点微调抖动"（Metric Jiggling）。

    核心逻辑：将当前固定跃点 base_metric（默认 10）短暂改为 base_metric+1
    （如 11），间隔几毫秒后立即改回 base_metric。通过动态抖动路由跃点，
    强行迫使 Windows 立即刷新并重置该网卡的路由缓存（Routing Cache），
    从而主动唤醒因 TCP 指数退避而陷入睡死状态的慢速链路，拉回并发大乱斗。

    所有命令均通过 InterfaceIndex 纯数字标识执行，杜绝中文乱码隐患。

    Args:
        if_index: 网卡接口索引（InterfaceIndex）
        base_metric: 网卡当前的固定跃点基准值（加速预设为 10）
        jiggle_delay_ms: 抖动到改回之间的间隔（毫秒），默认 5ms

    Returns:
        Tuple[bool, str]: (是否成功, 结果信息)
    """
    import time

    try:
        if not isinstance(if_index, int) or if_index <= 0:
            error_msg = f"无效的接口索引: {if_index}"
            logger.error(error_msg)
            return False, error_msg

        if not isinstance(base_metric, int) or base_metric < 1 or base_metric >= 9999:
            error_msg = f"无效的基准跃点数: {base_metric}"
            logger.error(error_msg)
            return False, error_msg

        jiggle_metric = base_metric + 1

        # 第一步：短暂抖动到 base_metric + 1
        ok_up, out_up = set_adapter_metric(if_index, metric=jiggle_metric, auto_metric=False)
        if not ok_up:
            return False, f"抖动阶段失败: {out_up}"

        # 第二步：短暂等待几毫秒后立即改回 base_metric
        time.sleep(max(0, jiggle_delay_ms) / 1000.0)

        ok_down, out_down = set_adapter_metric(if_index, metric=base_metric, auto_metric=False)
        if not ok_down:
            return False, f"复位阶段失败: {out_down}"

        msg = f"接口 {if_index} 完成跃点微调抖动 ({base_metric}->{jiggle_metric}->{base_metric})"
        logger.info(msg)
        return True, msg

    except Exception as e:
        error_msg = f"跃点微调抖动时发生异常: {type(e).__name__}: {str(e)}"
        logger.error(error_msg)
        return False, error_msg


