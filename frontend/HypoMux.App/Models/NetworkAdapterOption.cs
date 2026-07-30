using HypoMux.App.ViewModels;

namespace HypoMux.App.Models;

public sealed class NetworkAdapterOption : ObservableObject
{
    private bool _isSelected;
    private int _connections;
    private long _bytesUp;
    private long _bytesDown;
    private string _healthState = "idle";

    public required string Id { get; init; }

    public required string Name { get; init; }

    public required string SourceIp { get; init; }

    public int IfIndex { get; init; }

    public string SourceIpv6 { get; init; } = string.Empty;

    public int Ipv6IfIndex { get; init; }

    public bool IsWireless { get; init; }

    public IReadOnlyList<string> DnsServers { get; init; } = [];

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

    public string HealthState
    {
        get => _healthState;
        set => SetProperty(ref _healthState, value);
    }
}
