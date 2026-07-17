"""
HypoMux 国际化模块 - Python 原生字典查表法

无需 .qm 外部文件，兼容 Nuitka 单文件打包。
所有界面文本通过 tr(key) 函数统一获取，语言切换时由各组件的
retranslate_ui() 方法批量刷新 .setText()。
"""

from PySide6.QtCore import QSettings


# 全局双语映射字典
I18N_MAP = {
    "zh": {
        # === 主窗口 ===
        "window_title": "HypoMux - Windows 多网卡双协议分流加速",
        "subtitle": "多网卡 HTTP/HTTPS/SOCKS 分流引擎 · 实时分流大屏",
        "settings_btn": "设置",
        "status_loading": "状态: 正在加载网卡...",
        "status_no_adapters": "状态: 未找到可用的网卡",
        "status_loaded": "状态: 已加载 {count} 个网卡，就绪",
        "status_load_failed": "状态: 加载失败",
        "status_starting": "状态: 正在启动双协议分流引擎...",
        "status_stopping": "状态: 正在停止...",
        "status_stopped": "状态: 已停止，就绪",
        "status_start_failed": "状态: 启动失败",
        "status_running": "状态: 双协议分流引擎运行中 @ {endpoint}",
        "status_running_live": "状态: 分流运行中 · 下行 {down:.2f} MB/s · 连接 {conn}",

        # 表头
        "col_select": "选择",
        "col_alias": "网卡别名",
        "col_ipv4": "IPv4 地址",
        "col_speed": "实时速度 (MB/s)",
        "col_conn": "实时连接数",

        # 数据大屏
        "speed_caption": "合并下行总速度 (MB/s)",
        "up_format": "上行 {value:.2f} MB/s",
        "conn_format": "总连接数 {value}",

        # 操作栏
        "select_all": "全选",
        "deselect_all": "取消全选",
        "port_label": "SOCKS 端口",
        "boost_start": "一键加速",
        "boost_stop": "停止加速",

        # 警告/提示
        "warn_boosting_refresh": "加速运行中，请先停止再刷新网卡",
        "warn_no_adapters": "未找到任何可用的网卡",
        "warn_no_selection": "请先勾选至少一张拥有有效 IPv4 的网卡",
        "warn_steam_running": "检测到 Steam 正在运行，请重启 Steam 客户端以使多链路加速完全生效。",
        "warn_foreign_tun_route": "检测到第三方虚拟隧道 {name} 正在接管默认路由，请先关闭该代理或 VPN 后再启动虚拟网卡模式",
        "log_steam_running": "[警告] 检测到 Steam 正在运行，请重启 Steam 客户端以使多链路加速完全生效。",
        "error_load_adapters": "加载网卡失败:\n\n{error}",
        "error_start_failed": "分流引擎启动失败:\n\n{error}",
        "error_proxy_write": "双协议引擎已监听，但无法写入 Windows 系统代理:\n\n{error}",

        # InfoBar 标题
        "infobar_info": "提示",
        "infobar_success": "成功",
        "infobar_warning": "警告",
        "infobar_error": "错误",

        # === 设置面板 ===
        "settings_back": "< 返回主界面",
        "settings_title": "设置",
        "settings_global": "全局设置",
        "settings_language": "软件语言 (Language)",
        "settings_language_zh": "中文",
        "settings_language_en": "English",
        "settings_proxy_port": "本地代理端口",
        "settings_http_label": "HTTP:",
        "settings_socks_label": "SOCKS5:",
        "settings_close_behavior": "关闭行为",
        "settings_close_to_tray": "最小化到系统托盘",
        "settings_close_to_exit": "直接退出程序",
        "settings_autostart": "开机自动启动",
        "settings_autostart_hint": "开机时静默启动并最小化到系统托盘",
        "settings_autostart_on": "已开启开机自启",
        "settings_autostart_off": "已关闭开机自启",
        "settings_autostart_failed": "开机自启设置失败，请检查系统权限",
        "settings_about": "关于项目",
        "settings_version": "当前版本: v2.0.0",
        "settings_lang_saved": "界面语言已切换",
        "settings_sponsorship_title": "赞助支持",
        "settings_sponsorship_text": "HypoMux 是一个完全出于技术热情、由作者在业余时间独立开发与维护的开源项目，作者目前仍是在校学生，项目的深度开发与日常维护（如高频使用 AI 工具辅助重构、API 测试等）存在一定的实际开销。如果你觉得这个工具对你有帮助，欢迎请作者喝杯咖啡，支持本项目的持续迭代！\n\n温馨提示：量力而行。赞赏纯属自愿，无论是否赞赏，你都可以永久免费使用 HypoMux 的核心功能！\n\n赞助请留下您的昵称！",

        # === 网卡诊断（任务3）===
        "diag_btn": "网卡体检",
        "diag_running": "正在诊断 {name} ...",
        "diag_no_selection": "请先勾选至少一张拥有有效 IPv4 的网卡再进行体检",
        "diag_title": "链路体检报告",
        "diag_status_available": "可用",
        "diag_status_unstable": "不稳定",
        "diag_status_unavailable": "不可用",
        "diag_desc_available": "链路稳定，0 丢包，延迟抖动极低，可放心加入加速池。",
        "diag_desc_unstable": "链路存在波动：丢包或抖动偏高，加速时可能出现卡顿。",
        "diag_desc_unavailable": "链路不可用：100% 丢包或源 IP 绑定失败（可能是网卡被架空/掉线）。",
        "diag_metric_loss": "丢包率",
        "diag_metric_latency": "平均延迟",
        "diag_metric_jitter": "抖动",
        "diag_card_line": "{name} · {ip}",
        "diag_check_source_binding": "源地址绑定",
        "diag_check_gateway": "默认网关",
        "diag_check_dns": "DNS 配置",
        "diag_check_metric": "路由优先级",
        "diag_check_missing": "未配置或未读取到",
        "diag_check_metric_auto": "自动跃点",
        "diag_check_metric_fixed": "固定跃点",

        # === 侧边导航（第三阶段 Fluent 换装）===
        "nav_home": "首页",
        "nav_routing": "路由规则",
        "nav_tools": "网络体检",
        "nav_settings": "系统设置",
        "nav_about": "关于",
        "nav_blocked_domains": "单网卡被墙域名",

        # === 首页数据看板 ===
        "home_engine_title": "聚合分流引擎",
        "home_engine_on": "聚合引擎：已启用",
        "home_engine_off": "聚合引擎：未启用",
        "home_engine_control": "启用聚合引擎",
        "home_engine_control_hint": "启动后将使用已勾选网卡进行聚合分流。",
        "home_engine_switch_on": "已启用",
        "home_engine_switch_off": "未启用",
        "home_engine_transitioning": "正在处理…",
        "home_engine_ports": "SOCKS {socks} · HTTP/HTTPS {http}",
        "home_total_speed": "合并下行总速度",
        "home_speed_unit": "MB/s",
        "home_up_conn": "上行 {up:.2f} MB/s · 总连接 {conn}",
        "home_adapters_title": "网卡链路遥测",
        "home_no_adapters": "未发现可用网卡，请点击刷新重试",
        "home_refresh_tip": "重新扫描网卡",
        "home_select_all": "全选",
        "home_deselect_all": "取消全选",
        "home_card_speed": "{value:.2f} MB/s",
        "home_card_conn": "连接 {value}",
        "home_row_traffic": "{speed:.2f} MB/s · {conn} 条连接",
        "home_metric_connections": "活动连接数",
        "home_metric_connections_value": "{value} 个活动链路",
        "home_metric_accelerated": "本期已加速",
        "home_metric_latency": "节点抖动",
        "home_metric_latency_value": "{value} ms",
        "home_metric_kernel": "加速内核态",
        "home_kernel_idle": "未托管",
        "home_kernel_proxy": "系统代理托管",
        "home_kernel_tun": "TUN 驱动级托管",
        "home_health_unknown": "未体检",
        "home_log_title": "实时网络日志",
        "home_card_ppp": "拨号",

        # === 路由规则页 ===
        "routing_title": "进程级分流规则",
        "routing_hint": "为指定进程指派固定出口：单网卡降延迟，或聚合通道带宽叠加。未命中规则的流量默认走聚合通道。",
        "routing_col_process": "进程名",
        "routing_col_target": "目标地址",
        "routing_col_rule": "规则",
        "routing_col_nic": "出口通道",
        "routing_empty": "暂无分流规则",
        "routing_add": "添加规则",
        "routing_remove": "删除选中",
        "routing_select_process": "选择运行中进程",
        "routing_process_loading": "正在读取进程...",
        "routing_process_dialog_title": "选择运行中进程",
        "routing_process_search_placeholder": "搜索进程名",
        "routing_process_empty": "没有匹配的进程",
        "routing_dialog_ok": "确定",
        "routing_dialog_cancel": "取消",
        "routing_outbound_aggregation": "多卡聚合叠加",
        "routing_outbound_direct": "直连",
        "routing_placeholder_process": "例如 cs2.exe",
        "routing_duplicate_hint": "检测到重复进程规则：{names}。运行时将以第一条为准，请删除或修改重复项。",
        "routing_duplicate_process": "进程 {name} 已存在，请直接编辑原规则。",
        "routing_restart_required": "分流规则已保存，重启加速后生效。",
        "routing_backup_group": "备份与分享",
        "routing_list_title": "规则列表",
        "routing_rule_count": "共 {count} 条",
        "routing_export": "导出规则",
        "routing_import": "导入规则",
        "routing_export_title": "导出分流规则",
        "routing_import_title": "导入分流规则",
        "routing_backup_file_filter": "HypoMux 分流规则 (*.json)",
        "routing_export_success": "已导出 {count} 条分流规则。",
        "routing_export_failed": "导出分流规则失败：{error}",
        "routing_import_confirm_title": "确认导入分流规则",
        "routing_import_confirm_content": "将用导入的 {count} 条规则替换当前规则表，是否继续？",
        "routing_import_confirm": "导入并替换",
        "routing_import_success": "已导入 {count} 条分流规则。",
        "routing_import_failed": "导入分流规则失败：{error}",

        # === 运行模式 / TUN 虚拟网卡 ===
        "mode_label": "运行模式",
        "mode_proxy": "系统代理模式",
        "mode_tun": "虚拟网卡模式",
        "tun_need_admin_title": "需要管理员权限",
        "tun_need_admin_content": "虚拟网卡模式需要系统管理员权限以创建 TUN 加速设备。请右键点击 HypoMux 并选择「以管理员身份运行」，或者切换回「系统代理模式」继续使用。",
        "tun_starting": "正在启动虚拟网卡内核...",
        "tun_preflighting": "正在检查选中网卡…",
        "tun_started": "虚拟网卡内核已接管全局流量",
        "tun_validating": "正在验证虚拟网卡的 DNS 与 HTTPS 联网能力",
        "tun_validation_failed": "虚拟网卡联网验证失败，已自动停止并恢复网络设置",
        "tun_validation_timeout": "虚拟网卡联网验证超时，已自动停止并恢复网络设置",
        "tun_watchdog_rollback": "检测到物理网络正常但虚拟网卡连续无法联网，已自动停止加速并恢复网络设置",
        "tun_stopped": "虚拟网卡内核已停止",
        "tun_unexpected_stop": "虚拟网卡内核意外停止，已自动回滚并恢复网络设置",
        "tun_singbox_missing": "未找到 bin/sing-box.exe，无法启用虚拟网卡模式",
        "tun_pool_start_timeout": "出站池启动超时，已取消虚拟网卡接管",

        # === 网络体检页 ===
        "tools_title": "网卡链路体检",
        "tools_hint": "调用 Rust 诊断内核，对选中网卡逐张发射 ICMP 探针，评估丢包与抖动，给出红/黄/绿三级链路质量。",
        "tools_start": "开始网卡体检",
        "tools_running": "正在体检...",
        "tools_no_result": "尚未体检，点击上方按钮开始。",

        # === 系统设置页 ===
        "settings_theme": "界面主题",
        "settings_theme_hint": "切换浅色 / 深色 / 跟随系统",
        "settings_theme_light": "浅色",
        "settings_theme_dark": "深色",
        "settings_theme_auto": "跟随系统",
        "settings_config_group": "配置与启动",
        "settings_network_dns": "网络与 DNS 设置",
        "settings_advanced_network": "高级网络 / 特殊网络环境",
        "settings_blocked_domains_manage": "单网卡被墙域名记录",
        "settings_blocked_domains_manage_hint": "查看、刷新或清除已自动记录的域名。",
        "settings_blocked_domains_open": "管理",
        "settings_dns_server": "传统 DNS 兜底服务器",
        "settings_dns_fallback_hint": "仅在 DoH 不可用时作为 53 端口兜底使用",
        "settings_dns_placeholder": "例如 223.5.5.5",
        "settings_doh_policy": "DoH 解析策略",
        "settings_doh_auto": "自动优选",
        "settings_doh_alidns": "阿里 DNS",
        "settings_doh_dnspod": "腾讯 DNSPod",
        "settings_doh_google": "Google DNS",
        "settings_doh_hint": "虚拟网卡模式优先使用 HTTPS/443 DoH 解析；传统 DNS 仅作为 53 端口兜底。",
        "settings_dns_saved": "DNS 设置已保存",
        "settings_dns_invalid": "DNS 地址格式无效，请输入合法 IPv4 地址",
        "settings_dns_save_failed": "DNS 设置保存失败，请检查配置文件权限",
        "settings_config_path": "配置文件位置",
        "about_intro": "HypoMux 是一款面向 Windows 开源免费的多网卡双协议分流加速工具。",
        "about_notice_title": "网络与合规声明",
        "about_notice_text": "HypoMux 是一个透明、开源的网络工具，仅用于用户本人拥有授权的设备与网络连接，不应用于绕过第三方访问控制、网络限制、平台规则或任何未经授权的安全措施。\n\nHypoMux v2.0 会在运行时动态调整 Windows 系统代理与路由设置，以实现多网卡加速。加速流量将在本地进行安全代理与分流，其他流量将通过高级分流规则直接放行。\n\n软件在停止或卸载时会自动完整恢复所有系统网络设置。对于竞技类端游等对延迟极度敏感的应用，请确保它们已正确配置在直连分流规则中，或在游戏时暂停本工具。",
        "about_sponsorship_title": "赞助项目",
        "about_wechat": "微信支付",
        "about_alipay": "支付宝",
        "about_qr_missing": "收款码未找到",

        # === 系统托盘 ===
        "tray_show_main": "显示主界面",
        "tray_exit": "退出程序",
        "tray_tooltip": "HypoMux - 多网卡加速",

        # === 单网卡被墙域名页 ===
        "blocked_title": "单网卡被墙域名",
        "blocked_hint": "当某网卡连接目标域名失败、但其他网卡可连通时，自动记录该域名为该网卡的黑名单。后续分流将自动规避。",
        "blocked_enable": "启用自动分流清单",
        "blocked_enable_hint": "开启后与加速引擎捆绑生效；关闭后暂停规避功能。仅建议学校/企业有特殊需求的用户使用，普通家庭用户不建议使用。",
        "blocked_no_data": "暂无记录，在加速期间遇到连通失败的域名将在此显示",
        "blocked_nic_label": "网卡",
        "blocked_domain_count": "{count} 个被墙域名",
        "blocked_delete_domain": "移除此域名",
        "blocked_clear_all": "清空全部",
        "blocked_verify_blocked": "已确认被墙",
        "blocked_refresh": "刷新",

        # === 单网卡被墙域名页 - 过期机制 ===
        "blocked_expiry_toggle": "黑名单自动过期",
        "blocked_expiry_hint": "开启后黑名单条目 30 分钟后自动恢复。关闭后条目永不自动移除。",
        "blocked_permanent": "永久拦截",
        "blocked_expire_min": "{min} 分后恢复",
        "blocked_expire_sec": "{sec} 秒后恢复",

        # === 权重调度器 ===
        "home_weighted_toggle": "使用权重调度器",
        "home_weighted_status": "权重调度器：{state}",
        "home_weighted_hint": "按网卡相对权重分配新连接；默认权重相等。关闭后采用轮询均分。",
        "home_bw_column": "调度权重",
        "home_bw_column_hint": "相对比例：数值越大，分配到的新连接越多；默认均为 1，不会限制网卡速度。",
    },

    "en": {
        # === Main Window ===
        "window_title": "HypoMux - Multi-NIC Dual-Protocol Traffic Splitting",
        "subtitle": "Multi-NIC HTTP/HTTPS/SOCKS Splitting Engine · Live Dashboard",
        "settings_btn": "Settings",
        "status_loading": "Status: Loading network adapters...",
        "status_no_adapters": "Status: No available adapters found",
        "status_loaded": "Status: {count} adapter(s) loaded, ready",
        "status_load_failed": "Status: Load failed",
        "status_starting": "Status: Starting dual-protocol engine...",
        "status_stopping": "Status: Stopping...",
        "status_stopped": "Status: Stopped, ready",
        "status_start_failed": "Status: Start failed",
        "status_running": "Status: Dual-protocol engine running @ {endpoint}",
        "status_running_live": "Status: Running · Down {down:.2f} MB/s · Conn {conn}",

        # Table headers
        "col_select": "Select",
        "col_alias": "Adapter Alias",
        "col_ipv4": "IPv4 Address",
        "col_speed": "Speed (MB/s)",
        "col_conn": "Connections",

        # Dashboard
        "speed_caption": "Combined Download Speed (MB/s)",
        "up_format": "Up {value:.2f} MB/s",
        "conn_format": "Connections {value}",

        # Action bar
        "select_all": "Select All",
        "deselect_all": "Deselect All",
        "port_label": "SOCKS Port",
        "boost_start": "Boost",
        "boost_stop": "Stop",

        # Warnings / Messages
        "warn_boosting_refresh": "Boosting in progress, stop first before refreshing",
        "warn_no_adapters": "No available network adapters found",
        "warn_no_selection": "Please select at least one adapter with a valid IPv4",
        "warn_steam_running": "Steam is running. Please restart the Steam client for multi-link acceleration to take full effect.",
        "warn_foreign_tun_route": "Detected third-party virtual tunnel {name} taking over the default route. Close that proxy or VPN before starting Virtual NIC mode.",
        "log_steam_running": "[Warning] Steam is running. Please restart the Steam client for multi-link acceleration to take full effect.",
        "error_load_adapters": "Failed to load adapters:\n\n{error}",
        "error_start_failed": "Engine start failed:\n\n{error}",
        "error_proxy_write": "Engine is listening but cannot write system proxy:\n\n{error}",

        # InfoBar titles
        "infobar_info": "Info",
        "infobar_success": "Success",
        "infobar_warning": "Warning",
        "infobar_error": "Error",

        # === Settings Panel ===
        "settings_back": "< Back",
        "settings_title": "Settings",
        "settings_global": "Global Settings",
        "settings_language": "Language",
        "settings_language_zh": "Chinese",
        "settings_language_en": "English",
        "settings_proxy_port": "Local Proxy Ports",
        "settings_http_label": "HTTP:",
        "settings_socks_label": "SOCKS5:",
        "settings_close_behavior": "Close Behavior",
        "settings_close_to_tray": "Minimize to system tray",
        "settings_close_to_exit": "Exit program",
        "settings_autostart": "Launch at startup",
        "settings_autostart_hint": "Start silently and minimize to the system tray on boot",
        "settings_autostart_on": "Launch at startup enabled",
        "settings_autostart_off": "Launch at startup disabled",
        "settings_autostart_failed": "Failed to set launch at startup, check system permissions",
        "settings_about": "About",
        "settings_version": "Version: v2.0.0",
        "settings_lang_saved": "Language switched",
        "settings_sponsorship_title": "Sponsorship",
        "settings_sponsorship_text": "HypoMux is an open-source project developed and maintained independently by the author during their spare time, purely out of technical passion. The author is currently a student, and the in-depth development and daily maintenance of the project (such as high-frequency use of AI tools for refactoring, API testing, etc.) incur certain practical expenses. If you feel that this tool has effectively solved your network pain points, you are welcome to buy the author a cup of coffee to support the continuous iteration of this project!\n\nFriendly Reminder: Please act within your means. Sponsorship is purely voluntary. Whether you sponsor or not, you can use the core functions of HypoMux for free permanently!\n\nPlease leave your nickname when sponsoring!",

        # === Adapter Diagnostics (Task 3) ===
        "diag_btn": "Health Check",
        "diag_running": "Diagnosing {name} ...",
        "diag_no_selection": "Select at least one adapter with a valid IPv4 before the health check",
        "diag_title": "Link Health Report",
        "diag_status_available": "Available",
        "diag_status_unstable": "Unstable",
        "diag_status_unavailable": "Unavailable",
        "diag_desc_available": "Stable link, 0 packet loss, very low jitter. Safe to add to the boost pool.",
        "diag_desc_unstable": "Link fluctuates: packet loss or jitter is high, stutter may occur while boosting.",
        "diag_desc_unavailable": "Link unavailable: 100% packet loss or source IP bind failure (adapter may be down/superseded).",
        "diag_metric_loss": "Packet Loss",
        "diag_metric_latency": "Avg Latency",
        "diag_metric_jitter": "Jitter",
        "diag_card_line": "{name} · {ip}",
        "diag_check_source_binding": "Source Binding",
        "diag_check_gateway": "Default Gateway",
        "diag_check_dns": "DNS Configuration",
        "diag_check_metric": "Route Priority",
        "diag_check_missing": "Not configured or unavailable",
        "diag_check_metric_auto": "Automatic metric",
        "diag_check_metric_fixed": "Fixed metric",

        # === Side Navigation (Phase 3 Fluent revamp) ===
        "nav_home": "Dashboard",
        "nav_routing": "Split Tunneling",
        "nav_tools": "Diagnostics",
        "nav_settings": "Settings",
        "nav_about": "About",
        "nav_blocked_domains": "Blocked Domains",

        # === Home Dashboard ===
        "home_engine_title": "Aggregation Engine",
        "home_engine_on": "Engine: Enabled",
        "home_engine_off": "Engine: Disabled",
        "home_engine_control": "Enable aggregation engine",
        "home_engine_control_hint": "Use the selected adapters for aggregated traffic splitting.",
        "home_engine_switch_on": "Enabled",
        "home_engine_switch_off": "Disabled",
        "home_engine_transitioning": "Working…",
        "home_engine_ports": "SOCKS {socks} · HTTP/HTTPS {http}",
        "home_total_speed": "Combined Download Speed",
        "home_speed_unit": "MB/s",
        "home_up_conn": "Up {up:.2f} MB/s · Connections {conn}",
        "home_adapters_title": "Adapter Link Telemetry",
        "home_no_adapters": "No available adapters, click refresh to retry",
        "home_refresh_tip": "Rescan adapters",
        "home_select_all": "Select All",
        "home_deselect_all": "Deselect All",
        "home_card_speed": "{value:.2f} MB/s",
        "home_card_conn": "Conn {value}",
        "home_row_traffic": "{speed:.2f} MB/s · {conn} connections",
        "home_metric_connections": "Active Connections",
        "home_metric_connections_value": "{value} active links",
        "home_metric_accelerated": "Accelerated This Run",
        "home_metric_latency": "Node Jitter",
        "home_metric_latency_value": "{value} ms",
        "home_metric_kernel": "Kernel State",
        "home_kernel_idle": "Idle",
        "home_kernel_proxy": "System Proxy Managed",
        "home_kernel_tun": "TUN Driver Managed",
        "home_health_unknown": "Not Checked",
        "home_log_title": "Live Network Log",
        "home_card_ppp": "Dial-up",

        # === Routing Page ===
        "routing_title": "Per-Process Split Rules",
        "routing_hint": "Pin a process to a fixed outbound: a single NIC for low latency, or the aggregation channel for bandwidth stacking. Unmatched traffic defaults to the aggregation channel.",
        "routing_col_process": "Process",
        "routing_col_target": "Target",
        "routing_col_rule": "Rule",
        "routing_col_nic": "Outbound",
        "routing_empty": "No split rules yet",
        "routing_add": "Add Rule",
        "routing_remove": "Remove Selected",
        "routing_select_process": "Select Running Process",
        "routing_process_loading": "Loading processes...",
        "routing_process_dialog_title": "Select Running Process",
        "routing_process_search_placeholder": "Search process name",
        "routing_process_empty": "No matching processes",
        "routing_dialog_ok": "OK",
        "routing_dialog_cancel": "Cancel",
        "routing_outbound_aggregation": "Multi-NIC Aggregation",
        "routing_outbound_direct": "Direct",
        "routing_placeholder_process": "e.g. cs2.exe",
        "routing_duplicate_hint": "Duplicate process rules detected: {names}. The first rule takes precedence; remove or edit the duplicates.",
        "routing_duplicate_process": "Process {name} already exists. Edit the existing rule instead.",
        "routing_restart_required": "Split rules were saved and will apply after acceleration is restarted.",
        "routing_backup_group": "Backup & Share",
        "routing_list_title": "Rule List",
        "routing_rule_count": "{count} rule(s)",
        "routing_export": "Export Rules",
        "routing_import": "Import Rules",
        "routing_export_title": "Export Split Rules",
        "routing_import_title": "Import Split Rules",
        "routing_backup_file_filter": "HypoMux Split Rules (*.json)",
        "routing_export_success": "Exported {count} split rule(s).",
        "routing_export_failed": "Failed to export split rules: {error}",
        "routing_import_confirm_title": "Confirm Split-Rule Import",
        "routing_import_confirm_content": "Replace the current rule table with {count} imported rule(s)?",
        "routing_import_confirm": "Import and Replace",
        "routing_import_success": "Imported {count} split rule(s).",
        "routing_import_failed": "Failed to import split rules: {error}",

        # === Run Mode / TUN ===
        "mode_label": "Run Mode",
        "mode_proxy": "System Proxy Mode",
        "mode_tun": "Virtual NIC Mode",
        "tun_need_admin_title": "Administrator Required",
        "tun_need_admin_content": "Virtual NIC mode requires administrator privileges to create the TUN acceleration device. Right-click HypoMux and choose Run as administrator, or switch back to System Proxy Mode to continue.",
        "tun_starting": "Starting virtual NIC kernel...",
        "tun_preflighting": "Checking selected adapters…",
        "tun_started": "Virtual NIC kernel has taken over global traffic",
        "tun_validating": "Validating DNS and HTTPS connectivity through the virtual NIC",
        "tun_validation_failed": "Virtual NIC connectivity validation failed. Acceleration was stopped and network settings were restored.",
        "tun_validation_timeout": "Virtual NIC connectivity validation timed out. Acceleration was stopped and network settings were restored.",
        "tun_watchdog_rollback": "The physical network is available, but the virtual NIC repeatedly failed. Acceleration was stopped and network settings were restored.",
        "tun_stopped": "Virtual NIC kernel stopped",
        "tun_unexpected_stop": "Virtual NIC kernel stopped unexpectedly. Acceleration was rolled back and network settings were restored.",
        "tun_singbox_missing": "bin/sing-box.exe not found, cannot enable Virtual NIC mode",
        "tun_pool_start_timeout": "Outbound pool startup timed out; Virtual NIC takeover was cancelled",

        # === Diagnostics Page ===
        "tools_title": "Adapter Link Health Check",
        "tools_hint": "Invoke the Rust diagnostic kernel to send ICMP probes per selected adapter, measuring loss and jitter for a red/yellow/green quality grade.",
        "tools_start": "Start Health Check",
        "tools_running": "Checking...",
        "tools_no_result": "No results yet. Click the button above to start.",

        # === Settings Page ===
        "settings_theme": "Theme",
        "settings_theme_hint": "Switch between light / dark / follow system",
        "settings_theme_light": "Light",
        "settings_theme_dark": "Dark",
        "settings_theme_auto": "Follow System",
        "settings_config_group": "Config & Startup",
        "settings_network_dns": "Network and DNS Settings",
        "settings_advanced_network": "Advanced / Special Network Environment",
        "settings_blocked_domains_manage": "Per-NIC Blocked Domain Records",
        "settings_blocked_domains_manage_hint": "View, refresh, or clear automatically recorded domains.",
        "settings_blocked_domains_open": "Manage",
        "settings_dns_server": "Traditional DNS fallback server",
        "settings_dns_fallback_hint": "Used only as a port-53 fallback when DoH is unavailable",
        "settings_dns_placeholder": "Example 223.5.5.5",
        "settings_doh_policy": "DoH Resolution Policy",
        "settings_doh_auto": "Auto",
        "settings_doh_alidns": "AliDNS",
        "settings_doh_dnspod": "DNSPod",
        "settings_doh_google": "Google DNS",
        "settings_doh_hint": "TUN mode prefers HTTPS/443 DoH resolution; traditional DNS is only used as a port-53 fallback.",
        "settings_dns_saved": "DNS settings saved",
        "settings_dns_invalid": "Invalid DNS address, enter a valid IPv4 address",
        "settings_dns_save_failed": "Failed to save DNS settings, check config file permissions",
        "settings_config_path": "Config file location",
        "about_intro": "HypoMux is a multi-NIC dual-protocol traffic-splitting accelerator for Windows. It aggregates the bandwidth of multiple adapters (including PPPoE dial-up links) and ships a Rust diagnostic kernel that grades each link red/yellow/green.",
        "about_notice_title": "Network & Compliance Notice",
        "about_notice_text": "HypoMux is a transparent, open-source network utility intended only for authorized use on the user's own devices and network connections. It is not intended to bypass third-party access controls, network restrictions, platform rules, or security measures without authorization.\n\nHypoMux v2.0 temporarily modifies Windows system proxy and routing settings to enable multi-network acceleration. Accelerated traffic is securely proxied locally, while other traffic is bypassed via advanced split tunneling.\n\nAll changes are fully restored when the tool is stopped or uninstalled. For latency-sensitive applications such as competitive online games, please ensure they are correctly configured in the bypass rules or suspend the tool.",
        "about_sponsorship_title": "Sponsorship",
        "about_wechat": "WeChat Pay",
        "about_alipay": "Alipay",
        "about_qr_missing": "QR code not found",

        # === System Tray ===
        "tray_show_main": "Show Main Panel",
        "tray_exit": "Exit",
        "tray_tooltip": "HypoMux - Multi-NIC Acceleration",

        # === Blocked Domains Page ===
        "blocked_title": "Per-NIC Blocked Domains",
        "blocked_hint": "When a domain fails to connect on one NIC but works on others, it is automatically recorded as blocked for that NIC. Future connections will bypass the blocked NIC.",
        "blocked_enable": "Enable Auto Bypass List",
        "blocked_enable_hint": "Bundled with the acceleration engine when enabled; paused when disabled. Recommended only for school/enterprise users with special needs; not advised for regular home users.",
        "blocked_no_data": "No records yet. Domains that fail on some NICs during acceleration will appear here.",
        "blocked_nic_label": "NIC",
        "blocked_domain_count": "{count} blocked domain(s)",
        "blocked_delete_domain": "Remove this domain",
        "blocked_clear_all": "Clear All",
        "blocked_verify_blocked": "Confirmed Blocked",
        "blocked_refresh": "Refresh",

        # === Blocked Domains Page - Expiry ===
        "blocked_expiry_toggle": "Auto-Expire Blacklist",
        "blocked_expiry_hint": "When enabled, blocked entries auto-recover after 30 min. When disabled, entries never expire automatically.",
        "blocked_permanent": "Permanent",
        "blocked_expire_min": "Recover in {min}m",
        "blocked_expire_sec": "Recover in {sec}s",

        # === Weighted Scheduler ===
        "home_weighted_toggle": "Use Weighted Scheduler",
        "home_weighted_status": "Weighted scheduler: {state}",
        "home_weighted_hint": "Distribute new connections by relative NIC weight; all weights are equal by default. Disable for round-robin.",
        "home_bw_column": "Schedule Weight",
        "home_bw_column_hint": "Relative ratio: higher values receive more new connections. Default is 1 and this never rate-limits a NIC.",
    },
}


def get_language() -> str:
    """读取 QSettings 中保存的语言代码，默认 zh"""
    settings = QSettings("Hypostasis-Cat", "HypoMux")
    lang = settings.value("language", "zh")
    if lang not in I18N_MAP:
        lang = "zh"
    return lang


def tr(key: str, **kwargs) -> str:
    """根据当前语言获取翻译文本

    Args:
        key: I18N_MAP 中的键名
        **kwargs: 用于 str.format() 的动态参数

    Returns:
        翻译后的字符串，如果 key 不存在则原样返回 key
    """
    lang = get_language()
    text = I18N_MAP.get(lang, I18N_MAP["zh"]).get(key, key)
    if kwargs:
        try:
            return text.format(**kwargs)
        except (KeyError, ValueError):
            return text
    return text
