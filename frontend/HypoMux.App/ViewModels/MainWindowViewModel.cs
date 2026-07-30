using System.Collections.ObjectModel;
using System.IO;
using System.Net;
using System.Text.Json;
using HypoMux.App.Infrastructure;
using HypoMux.App.Models;
using HypoMux.App.Services;
using HypoMux.EngineClient;

namespace HypoMux.App.ViewModels;

public sealed class MainWindowViewModel : ObservableObject, IAsyncDisposable
{
    private readonly SynchronizationContext _uiContext;
    private readonly SemaphoreSlim _refreshGate = new(1, 1);
    private readonly SemaphoreSlim _lifecycleGate = new(1, 1);
    private readonly CancellationTokenSource _lifetime = new();
    private EngineProcessClient? _client;
    private Task? _pollingTask;
    private bool _isConnected;
    private bool _isAccelerating;
    private bool _useTunMode;
    private bool _weighted;
    private bool _strictRoute = true;
    private bool _closeToTray = true;
    private int _socksPort = 10800;
    private int _httpPort = 10801;
    private string _connectionState = "未连接";
    private string _engineVersion = "—";
    private string _engineState = "stopped";
    private string _tunState = "stopped";
    private string _platform = "—";
    private string _elevation = "—";
    private string _lastRefresh = "—";
    private string _healthSummary = "—";
    private string _diagnosticSourceIp = string.Empty;
    private string _diagnosticTargetIp = "223.5.5.5";
    private int _diagnosticCount = 10;
    private string _diagnosticResult = "尚未运行";
    private string _newRuleMatchType = "process";
    private string _newRuleValue = string.Empty;
    private string _newRuleOutbound = "direct";
    private RoutingRuleOption? _selectedRoutingRule;
    private string _downloadRateText = "0.00 MB/s";
    private string _uploadRateText = "0.00 MB/s";
    private string _sessionDataText = "0.00 MB";
    private string _jitterText = "0 ms";
    private int _totalConnections;
    private DateTimeOffset? _previousTelemetryAt;
    private long _previousTotalDown;
    private long _previousTotalUp;
    private readonly Dictionary<string, (long Down, long Up)> _previousAdapterTotals =
        new(StringComparer.OrdinalIgnoreCase);
    private bool _disposed;

    public MainWindowViewModel()
    {
        _uiContext = SynchronizationContext.Current
            ?? new SynchronizationContext();
        RefreshCommand = new AsyncRelayCommand(RefreshAsync, () => IsConnected);
        RunDiagnosticCommand = new AsyncRelayCommand(
            RunDiagnosticAsync,
            () => IsConnected);
        ReconnectCommand = new AsyncRelayCommand(
            ReconnectAsync,
            () => !IsAccelerating);
        StartCommand = new AsyncRelayCommand(
            StartAccelerationAsync,
            () => IsConnected && !IsAccelerating);
        StopCommand = new AsyncRelayCommand(
            StopAccelerationAsync,
            () => IsConnected && IsAccelerating);
        RescanAdaptersCommand = new RelayCommand(
            ScanAdapters,
            () => !IsAccelerating);
        SelectAllAdaptersCommand = new RelayCommand(
            () => SetAllAdaptersSelected(true),
            () => !IsAccelerating);
        ClearAdapterSelectionCommand = new RelayCommand(
            () => SetAllAdaptersSelected(false),
            () => !IsAccelerating);
        AddRoutingRuleCommand = new RelayCommand(AddRoutingRule);
        RemoveRoutingRuleCommand = new RelayCommand(
            RemoveRoutingRule,
            () => SelectedRoutingRule is not null);
        ClearLogsCommand = new RelayCommand(Logs.Clear);
    }

    public ObservableCollection<NetworkAdapterOption> Adapters { get; } = [];

    public ObservableCollection<string> Logs { get; } = [];

    public ObservableCollection<RoutingRuleOption> RoutingRules { get; } = [];

    public AsyncRelayCommand RefreshCommand { get; }

    public AsyncRelayCommand RunDiagnosticCommand { get; }

    public AsyncRelayCommand ReconnectCommand { get; }

    public AsyncRelayCommand StartCommand { get; }

    public AsyncRelayCommand StopCommand { get; }

    public RelayCommand RescanAdaptersCommand { get; }

    public RelayCommand SelectAllAdaptersCommand { get; }

    public RelayCommand ClearAdapterSelectionCommand { get; }

    public RelayCommand AddRoutingRuleCommand { get; }

    public RelayCommand RemoveRoutingRuleCommand { get; }

    public RelayCommand ClearLogsCommand { get; }

    public bool IsConnected
    {
        get => _isConnected;
        private set
        {
            if (SetProperty(ref _isConnected, value))
            {
                NotifyCommandStates();
            }
        }
    }

    public bool IsAccelerating
    {
        get => _isAccelerating;
        private set
        {
            if (SetProperty(ref _isAccelerating, value))
            {
                NotifyCommandStates();
            }
        }
    }

    public bool UseTunMode
    {
        get => _useTunMode;
        set => SetProperty(ref _useTunMode, value);
    }

    public bool Weighted
    {
        get => _weighted;
        set => SetProperty(ref _weighted, value);
    }

    public bool CloseToTray
    {
        get => _closeToTray;
        set => SetProperty(ref _closeToTray, value);
    }

    public bool StrictRoute
    {
        get => _strictRoute;
        set => SetProperty(ref _strictRoute, value);
    }

    public int SocksPort
    {
        get => _socksPort;
        set => SetProperty(ref _socksPort, Math.Clamp(value, 1, 65535));
    }

    public int HttpPort
    {
        get => _httpPort;
        set => SetProperty(ref _httpPort, Math.Clamp(value, 1, 65535));
    }

    public string ConnectionState
    {
        get => _connectionState;
        private set => SetProperty(ref _connectionState, value);
    }

    public string EngineVersion
    {
        get => _engineVersion;
        private set => SetProperty(ref _engineVersion, value);
    }

    public string EngineState
    {
        get => _engineState;
        private set => SetProperty(ref _engineState, value);
    }

    public string TunState
    {
        get => _tunState;
        private set => SetProperty(ref _tunState, value);
    }

    public string Platform
    {
        get => _platform;
        private set => SetProperty(ref _platform, value);
    }

    public string Elevation
    {
        get => _elevation;
        private set => SetProperty(ref _elevation, value);
    }

    public string LastRefresh
    {
        get => _lastRefresh;
        private set => SetProperty(ref _lastRefresh, value);
    }

    public string HealthSummary
    {
        get => _healthSummary;
        private set => SetProperty(ref _healthSummary, value);
    }

    public string DiagnosticSourceIp
    {
        get => _diagnosticSourceIp;
        set => SetProperty(ref _diagnosticSourceIp, value.Trim());
    }

    public string DiagnosticTargetIp
    {
        get => _diagnosticTargetIp;
        set => SetProperty(ref _diagnosticTargetIp, value.Trim());
    }

    public int DiagnosticCount
    {
        get => _diagnosticCount;
        set => SetProperty(ref _diagnosticCount, Math.Clamp(value, 1, 20));
    }

    public string DiagnosticResult
    {
        get => _diagnosticResult;
        private set => SetProperty(ref _diagnosticResult, value);
    }

    public string DownloadRateText
    {
        get => _downloadRateText;
        private set => SetProperty(ref _downloadRateText, value);
    }

    public string UploadRateText
    {
        get => _uploadRateText;
        private set => SetProperty(ref _uploadRateText, value);
    }

    public string SessionDataText
    {
        get => _sessionDataText;
        private set => SetProperty(ref _sessionDataText, value);
    }

    public string JitterText
    {
        get => _jitterText;
        private set => SetProperty(ref _jitterText, value);
    }

    public int TotalConnections
    {
        get => _totalConnections;
        private set => SetProperty(ref _totalConnections, value);
    }

    public string NewRuleMatchType
    {
        get => _newRuleMatchType;
        set => SetProperty(ref _newRuleMatchType, value);
    }

    public string NewRuleValue
    {
        get => _newRuleValue;
        set => SetProperty(ref _newRuleValue, value);
    }

    public string NewRuleOutbound
    {
        get => _newRuleOutbound;
        set => SetProperty(ref _newRuleOutbound, value);
    }

    public RoutingRuleOption? SelectedRoutingRule
    {
        get => _selectedRoutingRule;
        set
        {
            if (SetProperty(ref _selectedRoutingRule, value))
            {
                RemoveRoutingRuleCommand.NotifyCanExecuteChanged();
            }
        }
    }

    public async Task InitializeAsync()
    {
        var settings = AppSettingsStore.Load();
        UseTunMode = string.Equals(
            settings.Mode,
            "tun",
            StringComparison.OrdinalIgnoreCase);
        SocksPort = settings.SocksPort;
        HttpPort = settings.HttpPort;
        Weighted = settings.Weighted;
        StrictRoute = settings.StrictRoute;
        CloseToTray = settings.CloseToTray;
        foreach (var rule in settings.RoutingRules ?? [])
        {
            RoutingRules.Add(new RoutingRuleOption
            {
                MatchType = rule.MatchType,
                Value = rule.Value,
                Outbound = rule.Outbound,
            });
        }
        ScanAdapters(
            settings.SelectedAdapterIds ?? [],
            settings.AdapterWeights ?? new Dictionary<string, int>());
        try
        {
            SystemProxyService.RestoreIfOwned();
        }
        catch (Exception exception)
        {
            AppendLog($"恢复上次系统代理失败：{exception.Message}");
        }

        await ConnectAsync();
        _pollingTask ??= PollAsync(_lifetime.Token);
    }

    public async Task StartAccelerationAsync()
    {
        await _lifecycleGate.WaitAsync(_lifetime.Token);
        try
        {
            if (IsAccelerating)
            {
                return;
            }

            var client = RequireClient();
            var selected = Adapters.Where(adapter => adapter.IsSelected).ToArray();
            if (selected.Length == 0)
            {
                AppendLog("请至少选择一张活动网卡。");
                return;
            }

            SaveSettings();
            AppendLog(
                UseTunMode
                    ? $"正在启动 Go TUN 聚合（{selected.Length} 张网卡）…"
                    : $"正在启动 Go 系统代理（{selected.Length} 张网卡）…");
            if (UseTunMode)
            {
                await StartTunAsync(client, selected);
            }
            else
            {
                await StartProxyAsync(client, selected);
            }

            IsAccelerating = true;
            DiagnosticSourceIp = selected[0].SourceIp;
            AppendLog("加速已启动。");
            await RefreshAsync();
        }
        catch (Exception exception)
        {
            AppendLog($"启动失败：{exception.Message}");
            await RollBackStartAsync();
        }
        finally
        {
            _lifecycleGate.Release();
        }
    }

    public async Task StopAccelerationAsync()
    {
        await _lifecycleGate.WaitAsync();
        try
        {
            if (!IsAccelerating)
            {
                return;
            }

            var client = _client;
            if (client is not null && client.IsRunning)
            {
                try
                {
                    await client.RequestAsync<TunLifecycleResultDto>(
                        "tun.deactivate",
                        timeout: TimeSpan.FromSeconds(25));
                }
                catch (EngineRemoteException exception)
                    when (exception.Code == "invalid_state")
                {
                }

                try
                {
                    await client.RequestAsync<EngineStopResultDto>(
                        "engine.stop",
                        timeout: TimeSpan.FromSeconds(25));
                }
                catch (EngineRemoteException exception)
                    when (exception.Code == "invalid_state")
                {
                }
            }

            try
            {
                SystemProxyService.Disable();
            }
            catch (Exception exception)
            {
                AppendLog($"系统代理恢复失败：{exception.Message}");
            }

            IsAccelerating = false;
            EngineState = "stopped";
            TunState = "stopped";
            ClearTelemetry();
            AppendLog("加速已停止，系统代理已恢复。");
        }
        finally
        {
            _lifecycleGate.Release();
        }
    }

    private async Task ReconnectAsync()
    {
        if (_client is not null)
        {
            await _client.DisposeAsync();
            _client = null;
        }

        IsConnected = false;
        await ConnectAsync();
    }

    private async Task ConnectAsync()
    {
        ConnectionState = "正在连接";
        var executable = EngineExecutableResolver.Resolve();
        if (executable is null)
        {
            ConnectionState = "未找到 Go 引擎";
            AppendLog(
                "未找到 hypomux-engine.exe；可设置 HYPOMUX_ENGINE_PATH。");
            return;
        }

        var client = new EngineProcessClient(executable);
        client.EventReceived += OnEngineEvent;
        client.StandardErrorReceived += OnEngineStandardError;
        client.Disconnected += OnEngineDisconnected;
        _client = client;
        try
        {
            var hello = await client.StartAsync(_lifetime.Token);
            EngineVersion = $"{hello.EngineVersion} ({ShortCommit(hello.Commit)})";
            Platform = $"{hello.Os}/{hello.Arch} · PID {hello.Pid}";
            Elevation = hello.Elevated ? "管理员" : "普通权限";
            ConnectionState = "已连接";
            IsConnected = true;
            AppendLog(
                $"协议协商完成：v{hello.ProtocolVersion}，{hello.Capabilities.Count} 项能力。");
            await RefreshAsync();
        }
        catch (Exception exception)
        {
            ConnectionState = "连接失败";
            AppendLog($"连接失败：{exception.Message}");
            await client.DisposeAsync();
            if (ReferenceEquals(_client, client))
            {
                _client = null;
            }
        }
    }

    private async Task StartProxyAsync(
        EngineProcessClient client,
        IReadOnlyList<NetworkAdapterOption> selected)
    {
        await client.RequestAsync<EngineStartResultDto>(
            "engine.start",
            new
            {
                mode = "proxy",
                listen_host = "127.0.0.1",
                socks_port = SocksPort,
                http_port = HttpPort,
                weighted = Weighted,
                connect_timeout_ms = 6000,
                dns = BuildDnsStartConfig(selected),
                adapters = BuildAdapterParams(selected),
            },
            timeout: TimeSpan.FromSeconds(15),
            cancellationToken: _lifetime.Token);
        SystemProxyService.Enable(HttpPort, SocksPort);
    }

    private async Task StartTunAsync(
        EngineProcessClient client,
        IReadOnlyList<NetworkAdapterOption> selected)
    {
        var wired = selected.Where(adapter => !adapter.IsWireless).ToArray();
        var wireless = selected.Where(adapter => adapter.IsWireless).ToArray();
        var result = await client.RequestAsync<EngineStartResultDto>(
            "engine.start",
            new
            {
                mode = "tun_tcp_pool",
                listen_host = "127.0.0.1",
                weighted = Weighted,
                connect_timeout_ms = 6000,
                dns = BuildDnsStartConfig(selected),
                adapters = BuildAdapterParams(selected),
                channels = new object[]
                {
                    new
                    {
                        name = "nic_ethernet",
                        port = 0,
                        adapter_names = (wired.Length > 0 ? wired : selected)
                            .Select(adapter => adapter.Name)
                            .ToArray(),
                    },
                    new
                    {
                        name = "nic_wifi",
                        port = 0,
                        adapter_names = (wireless.Length > 0 ? wireless : selected)
                            .Select(adapter => adapter.Name)
                            .ToArray(),
                    },
                    new
                    {
                        name = "aggregation",
                        port = 0,
                        adapter_names = selected
                            .Select(adapter => adapter.Name)
                            .ToArray(),
                    },
                },
            },
            timeout: TimeSpan.FromSeconds(15),
            cancellationToken: _lifetime.Token);

        var dnsResult = await client.RequestAsync<DnsResolveResultDto>(
            "dns.resolve",
            new
            {
                domain = "www.msftconnecttest.com",
                adapter = selected[0].Name,
                record_type = "A",
            },
            timeout: TimeSpan.FromSeconds(10),
            cancellationToken: _lifetime.Token);
        var channels = result.Endpoints.Channels
            ?? throw new InvalidOperationException(
                "Go engine returned no TUN channel endpoints.");
        var configPath = SingBoxConfigBuilder.Write(
            channels,
            selected[0],
            dnsResult,
            RoutingRules,
            StrictRoute);
        var singBox = RuntimeAssetResolver.Resolve("sing-box.exe")
            ?? throw new FileNotFoundException(
                "bin/sing-box.exe was not found.");
        var activation = await client.RequestAsync<TunLifecycleResultDto>(
            "tun.activate",
            new
            {
                executable = singBox,
                config_path = configPath,
                startup_timeout_ms = 1500,
            },
            timeout: TimeSpan.FromSeconds(40),
            cancellationToken: _lifetime.Token);
        if (!string.Equals(
                activation.Tun.State,
                "running",
                StringComparison.OrdinalIgnoreCase))
        {
            throw new InvalidOperationException(
                "Go engine did not confirm a stable TUN sidecar.");
        }
    }

    private async Task RollBackStartAsync()
    {
        try
        {
            SystemProxyService.Disable();
        }
        catch (Exception)
        {
        }

        var client = _client;
        if (client is not null && client.IsRunning)
        {
            try
            {
                await client.RequestAsync<EngineStopResultDto>(
                    "engine.stop",
                    timeout: TimeSpan.FromSeconds(25));
            }
            catch (Exception rollbackException)
            {
                AppendLog($"启动回滚警告：{rollbackException.Message}");
            }
        }

        IsAccelerating = false;
    }

    private async Task RefreshAsync()
    {
        var client = _client;
        if (client is null || !client.IsRunning
            || !await _refreshGate.WaitAsync(0))
        {
            return;
        }

        try
        {
            var status = await client.RequestAsync<EngineStatusDto>(
                "engine.status",
                cancellationToken: _lifetime.Token);
            var health = await client.RequestAsync<HealthCheckDto>(
                "health.check",
                cancellationToken: _lifetime.Token);
            var tun = await client.RequestAsync<TunStatusDto>(
                "tun.status",
                cancellationToken: _lifetime.Token);
            EngineState = status.Engine.State;
            TunState = tun.State;
            IsAccelerating = string.Equals(
                status.Engine.State,
                "running",
                StringComparison.OrdinalIgnoreCase);
            HealthSummary = health.Ok
                ? $"正常 · 宿主运行 {FormatDuration(health.HostUptimeMs)}"
                : "异常";

            if (IsAccelerating)
            {
                var telemetry = await client.RequestAsync<EngineTelemetryDto>(
                    "engine.telemetry",
                    new { include_connections = true },
                    cancellationToken: _lifetime.Token);
                UpdateTelemetry(telemetry);
            }
            else
            {
                ClearTelemetry();
            }

            LastRefresh = DateTimeOffset.Now.ToString("HH:mm:ss");
        }
        catch (OperationCanceledException)
            when (_lifetime.IsCancellationRequested)
        {
        }
        catch (Exception exception)
        {
            AppendLog($"刷新失败：{exception.Message}");
        }
        finally
        {
            _refreshGate.Release();
        }
    }

    private async Task RunDiagnosticAsync()
    {
        var client = _client;
        if (client is null || !client.IsRunning)
        {
            return;
        }

        if (!IPAddress.TryParse(DiagnosticSourceIp, out _)
            || !IPAddress.TryParse(DiagnosticTargetIp, out _))
        {
            DiagnosticResult = "请输入有效的源 IP 和目标 IP。";
            return;
        }

        DiagnosticResult = "正在诊断…";
        try
        {
            var result = await client.RequestAsync<DiagnosticResultDto>(
                "diagnostic.run",
                new
                {
                    src_ip = DiagnosticSourceIp,
                    target_ip = DiagnosticTargetIp,
                    count = DiagnosticCount,
                    timeout_ms = 1000,
                },
                timeout: TimeSpan.FromSeconds(30),
                cancellationToken: _lifetime.Token);
            DiagnosticResult =
                $"状态：{result.Status}\n"
                + $"丢包：{result.LossRate:0.##}%\n"
                + $"平均延迟：{result.AvgLatencyMs:0.##} ms\n"
                + $"抖动：{result.JitterMs:0.##} ms\n"
                + $"收发：{result.Received}/{result.Sent}\n"
                + result.Note;
            JitterText = $"{result.JitterMs:0.##} ms";
        }
        catch (Exception exception)
        {
            DiagnosticResult = $"诊断失败：{exception.Message}";
        }
    }

    private async Task PollAsync(CancellationToken cancellationToken)
    {
        using var timer = new PeriodicTimer(TimeSpan.FromSeconds(2));
        try
        {
            while (await timer.WaitForNextTickAsync(cancellationToken))
            {
                if (IsConnected)
                {
                    await RefreshAsync();
                }
            }
        }
        catch (OperationCanceledException)
            when (cancellationToken.IsCancellationRequested)
        {
        }
    }

    private void ScanAdapters()
    {
        var selected = Adapters
            .Where(adapter => adapter.IsSelected)
            .Select(adapter => adapter.Id)
            .ToArray();
        var weights = Adapters.ToDictionary(
            adapter => adapter.Id,
            adapter => adapter.Weight,
            StringComparer.OrdinalIgnoreCase);
        ScanAdapters(selected, weights);
    }

    private void ScanAdapters(
        IReadOnlyList<string> selectedIds,
        IReadOnlyDictionary<string, int> weights)
    {
        var selected = selectedIds.ToHashSet(StringComparer.OrdinalIgnoreCase);
        var found = NetworkAdapterService.GetActiveAdapters();
        Adapters.Clear();
        foreach (var adapter in found)
        {
            adapter.IsSelected = selected.Count == 0 || selected.Contains(adapter.Id);
            if (weights.TryGetValue(adapter.Id, out var weight))
            {
                adapter.Weight = weight;
            }
            Adapters.Add(adapter);
        }

        if (string.IsNullOrWhiteSpace(DiagnosticSourceIp))
        {
            DiagnosticSourceIp = found.FirstOrDefault()?.SourceIp ?? string.Empty;
        }
    }

    private void SetAllAdaptersSelected(bool selected)
    {
        foreach (var adapter in Adapters)
        {
            adapter.IsSelected = selected;
        }
    }

    private void SaveSettings()
    {
        try
        {
            AppSettingsStore.Save(new AppSettings(
                Mode: UseTunMode ? "tun" : "proxy",
                SocksPort,
                HttpPort,
                Weighted,
                StrictRoute,
                CloseToTray,
                Adapters
                    .Where(adapter => adapter.IsSelected)
                    .Select(adapter => adapter.Id)
                    .ToArray(),
                Adapters.ToDictionary(
                    adapter => adapter.Id,
                    adapter => adapter.Weight,
                    StringComparer.OrdinalIgnoreCase),
                RoutingRules
                    .Where(rule => !string.IsNullOrWhiteSpace(rule.Value))
                    .Select(rule => new RoutingRuleSetting(
                        rule.MatchType,
                        rule.Value,
                        rule.Outbound))
                    .ToArray()));
        }
        catch (Exception exception)
        {
            AppendLog($"保存设置失败：{exception.Message}");
        }
    }

    private void AddRoutingRule()
    {
        var value = NewRuleValue.Trim();
        var matchType = NewRuleMatchType.Trim().ToLowerInvariant();
        var outbound = NewRuleOutbound.Trim().ToLowerInvariant();
        if (value.Length == 0
            || matchType is not ("process" or "domain" or "ip_cidr")
            || outbound is not (
                "direct"
                or "nic_ethernet"
                or "nic_wifi"
                or "aggregation"))
        {
            AppendLog("分流规则无效，请检查匹配类型、值和出站。");
            return;
        }

        RoutingRules.Add(new RoutingRuleOption
        {
            MatchType = matchType,
            Value = value,
            Outbound = outbound,
        });
        NewRuleValue = string.Empty;
    }

    private void RemoveRoutingRule()
    {
        if (SelectedRoutingRule is null)
        {
            return;
        }

        RoutingRules.Remove(SelectedRoutingRule);
        SelectedRoutingRule = null;
    }

    private static object[] BuildAdapterParams(
        IReadOnlyList<NetworkAdapterOption> selected) =>
        selected
            .Select(adapter => (object)new
            {
                name = adapter.Name,
                source_ip = adapter.SourceIp,
                if_index = adapter.IfIndex,
                source_ipv6 = adapter.SourceIpv6,
                ipv6_if_index = adapter.Ipv6IfIndex,
                weight = adapter.Weight,
                dns_servers = adapter.DnsServers,
            })
            .ToArray();

    private static object BuildDnsStartConfig(
        IReadOnlyList<NetworkAdapterOption> selected) =>
        new
        {
            policy = "auto",
            legacy_servers = selected
                .SelectMany(adapter => adapter.DnsServers)
                .Concat(["223.5.5.5", "119.29.29.29"])
                .Distinct(StringComparer.Ordinal)
                .ToArray(),
            cache_ttl_ms = 180000,
            query_timeout_ms = 4000,
        };

    private EngineProcessClient RequireClient() =>
        _client is { IsRunning: true } client
            ? client
            : throw new InvalidOperationException("Go engine is not connected.");

    private void OnEngineEvent(object? sender, EngineEventDto engineEvent)
    {
        _uiContext.Post(
            _ =>
            {
                if (engineEvent.Name == "log.record")
                {
                    var log = Deserialize<EngineLogDto>(engineEvent.Data);
                    AppendLog(
                        log is null
                            ? engineEvent.Name
                            : $"[{log.Component}] {log.Message}");
                    return;
                }

                if (engineEvent.Name == "engine.state_changed")
                {
                    var state = Deserialize<EngineStateDto>(engineEvent.Data);
                    if (state is not null)
                    {
                        EngineState = state.State;
                    }
                }
                else if (engineEvent.Name == "tun.state_changed")
                {
                    var tun = Deserialize<TunStatusDto>(engineEvent.Data);
                    if (tun is not null)
                    {
                        TunState = tun.State;
                    }
                }

                AppendLog($"事件 #{engineEvent.Sequence}：{engineEvent.Name}");
            },
            null);
    }

    private void OnEngineStandardError(object? sender, string line) =>
        _uiContext.Post(_ => AppendLog($"[engine:stderr] {line}"), null);

    private void OnEngineDisconnected(object? sender, Exception? exception) =>
        _uiContext.Post(
            _ =>
            {
                IsConnected = false;
                IsAccelerating = false;
                ConnectionState = _disposed ? "已关闭" : "连接已断开";
                try
                {
                    SystemProxyService.Disable();
                }
                catch (Exception restoreException)
                {
                    AppendLog($"系统代理恢复失败：{restoreException.Message}");
                }

                if (exception is not null && !_disposed)
                {
                    AppendLog($"引擎断开：{exception.Message}");
                }
            },
            null);

    private void UpdateTelemetry(EngineTelemetryDto telemetry)
    {
        var elapsedSeconds = _previousTelemetryAt is null
            ? 0
            : Math.Max(
                (telemetry.SampledAt - _previousTelemetryAt.Value).TotalSeconds,
                0);
        var byName = telemetry.Adapters.ToDictionary(
            item => item.Name,
            StringComparer.OrdinalIgnoreCase);
        foreach (var adapter in Adapters)
        {
            if (byName.TryGetValue(adapter.Name, out var item))
            {
                if (elapsedSeconds > 0
                    && _previousAdapterTotals.TryGetValue(
                        adapter.Name,
                        out var previous))
                {
                    adapter.DownloadRate =
                        Math.Max(item.BytesDown - previous.Down, 0)
                        / elapsedSeconds;
                    adapter.UploadRate =
                        Math.Max(item.BytesUp - previous.Up, 0)
                        / elapsedSeconds;
                }
                else
                {
                    adapter.DownloadRate = 0;
                    adapter.UploadRate = 0;
                }

                adapter.Connections = item.Connections;
                adapter.BytesUp = item.BytesUp;
                adapter.BytesDown = item.BytesDown;
                adapter.HealthState = item.HealthState;
                _previousAdapterTotals[adapter.Name] =
                    (item.BytesDown, item.BytesUp);
            }
            else
            {
                adapter.Connections = 0;
                adapter.BytesUp = 0;
                adapter.BytesDown = 0;
                adapter.DownloadRate = 0;
                adapter.UploadRate = 0;
                adapter.HealthState = "idle";
            }
        }

        var downRate = elapsedSeconds > 0
            ? Math.Max(telemetry.Total.BytesDown - _previousTotalDown, 0)
                / elapsedSeconds
            : 0;
        var upRate = elapsedSeconds > 0
            ? Math.Max(telemetry.Total.BytesUp - _previousTotalUp, 0)
                / elapsedSeconds
            : 0;
        DownloadRateText = FormatRate(downRate);
        UploadRateText = FormatRate(upRate);
        SessionDataText = FormatBytes(
            telemetry.Total.BytesDown + telemetry.Total.BytesUp);
        TotalConnections = telemetry.Total.Connections;
        _previousTelemetryAt = telemetry.SampledAt;
        _previousTotalDown = telemetry.Total.BytesDown;
        _previousTotalUp = telemetry.Total.BytesUp;
    }

    private void ClearTelemetry()
    {
        foreach (var adapter in Adapters)
        {
            adapter.Connections = 0;
            adapter.BytesUp = 0;
            adapter.BytesDown = 0;
            adapter.DownloadRate = 0;
            adapter.UploadRate = 0;
            adapter.HealthState = "idle";
        }

        DownloadRateText = "0.00 MB/s";
        UploadRateText = "0.00 MB/s";
        SessionDataText = "0.00 MB";
        TotalConnections = 0;
        _previousTelemetryAt = null;
        _previousTotalDown = 0;
        _previousTotalUp = 0;
        _previousAdapterTotals.Clear();
    }

    private void AppendLog(string message)
    {
        Logs.Add($"{DateTimeOffset.Now:HH:mm:ss}  {message}");
        while (Logs.Count > 500)
        {
            Logs.RemoveAt(0);
        }
    }

    private void NotifyCommandStates()
    {
        RefreshCommand.NotifyCanExecuteChanged();
        RunDiagnosticCommand.NotifyCanExecuteChanged();
        ReconnectCommand.NotifyCanExecuteChanged();
        StartCommand.NotifyCanExecuteChanged();
        StopCommand.NotifyCanExecuteChanged();
        RescanAdaptersCommand.NotifyCanExecuteChanged();
        SelectAllAdaptersCommand.NotifyCanExecuteChanged();
        ClearAdapterSelectionCommand.NotifyCanExecuteChanged();
    }

    private static T? Deserialize<T>(JsonElement element)
    {
        try
        {
            return element.Deserialize<T>(
                new JsonSerializerOptions
                {
                    PropertyNamingPolicy = JsonNamingPolicy.SnakeCaseLower,
                    PropertyNameCaseInsensitive = true,
                });
        }
        catch (JsonException)
        {
            return default;
        }
    }

    private static string ShortCommit(string commit) =>
        commit.Length > 7 ? commit[..7] : commit;

    private static string FormatDuration(long milliseconds)
    {
        var duration = TimeSpan.FromMilliseconds(Math.Max(milliseconds, 0));
        return duration.TotalHours >= 1
            ? $"{(int)duration.TotalHours}h {duration.Minutes}m"
            : $"{duration.Minutes}m {duration.Seconds}s";
    }

    private static string FormatRate(double bytesPerSecond) =>
        bytesPerSecond >= 1024 * 1024
            ? $"{bytesPerSecond / (1024 * 1024):0.00} MB/s"
            : bytesPerSecond >= 1024
                ? $"{bytesPerSecond / 1024:0.0} KB/s"
                : $"{bytesPerSecond:0} B/s";

    private static string FormatBytes(long bytes) =>
        bytes >= 1024L * 1024 * 1024
            ? $"{bytes / (1024d * 1024 * 1024):0.00} GB"
            : $"{bytes / (1024d * 1024):0.00} MB";

    public async ValueTask DisposeAsync()
    {
        if (_disposed)
        {
            return;
        }

        try
        {
            await StopAccelerationAsync();
        }
        catch (Exception exception)
        {
            AppendLog($"退出清理警告：{exception.Message}");
        }

        SaveSettings();
        _disposed = true;
        _lifetime.Cancel();
        if (_pollingTask is not null)
        {
            try
            {
                await _pollingTask;
            }
            catch (OperationCanceledException)
            {
            }
        }

        if (_client is not null)
        {
            await _client.DisposeAsync();
            _client = null;
        }

        IsConnected = false;
        ConnectionState = "已关闭";
        _lifetime.Dispose();
        _refreshGate.Dispose();
        _lifecycleGate.Dispose();
    }
}
