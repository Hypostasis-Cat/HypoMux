using HypoMux.App.ViewModels;

namespace HypoMux.App.Models;

public sealed class RoutingRuleOption : ObservableObject
{
    private string _matchType = "process";
    private string _value = string.Empty;
    private string _outbound = "direct";

    public string MatchType
    {
        get => _matchType;
        set => SetProperty(ref _matchType, value.Trim().ToLowerInvariant());
    }

    public string Value
    {
        get => _value;
        set => SetProperty(ref _value, value.Trim());
    }

    public string Outbound
    {
        get => _outbound;
        set => SetProperty(ref _outbound, value.Trim().ToLowerInvariant());
    }
}
