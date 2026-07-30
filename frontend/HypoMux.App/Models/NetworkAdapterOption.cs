using HypoMux.App.ViewModels;

namespace HypoMux.App.Models;

public sealed class NetworkAdapterOption : ObservableObject
{
    private bool _isSelected;
    private int _connections;
    private long _bytesUp;
    private long _bytesDown;
    private double _downloadRate;
    private double _uploadRate;
    private int _weight = 1;
    private string _healthState = "idle";

    public required string Id { get; init; }

    public required string Name { get; init; }

    public required string SourceIp { get; init; }

    public int IfIndex { get; init; }

    public string SourceIpv6 { get; init; } = string.Empty;

    public int Ipv6IfIndex { get; init; }

    public bool IsWireless { get; init; }

    public IReadOnlyList<string> DnsServers { get; init; } = [];

    public int Weight
    {
        get => _weight;
        set => SetProperty(ref _weight, Math.Clamp(value, 1, 100));
    }

    public bool IsSelected
    {
        get => _isSelected;
        set => SetProperty(ref _isSelected, value);
    }

    public int Connections
    {
        get => _connections;
        set => SetProperty(ref _connections, value);
    }

    public long BytesUp
    {
        get => _bytesUp;
        set => SetProperty(ref _bytesUp, value);
    }

    public long BytesDown
    {
        get => _bytesDown;
        set => SetProperty(ref _bytesDown, value);
    }

    public double DownloadRate
    {
        get => _downloadRate;
        set
        {
            if (SetProperty(ref _downloadRate, Math.Max(value, 0)))
            {
                OnPropertyChanged(nameof(TrafficSummary));
            }
        }
    }

    public double UploadRate
    {
        get => _uploadRate;
        set
        {
            if (SetProperty(ref _uploadRate, Math.Max(value, 0)))
            {
                OnPropertyChanged(nameof(TrafficSummary));
            }
        }
    }

    public string TrafficSummary =>
        $"{FormatRate(DownloadRate)} ↓  {FormatRate(UploadRate)} ↑";

    public string HealthState
    {
        get => _healthState;
        set => SetProperty(ref _healthState, value);
    }

    private static string FormatRate(double bytesPerSecond) =>
        bytesPerSecond >= 1024 * 1024
            ? $"{bytesPerSecond / (1024 * 1024):0.00} MB/s"
            : bytesPerSecond >= 1024
                ? $"{bytesPerSecond / 1024:0.0} KB/s"
                : $"{bytesPerSecond:0} B/s";
}
