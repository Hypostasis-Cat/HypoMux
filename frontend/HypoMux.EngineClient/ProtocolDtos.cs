using System.Text.Json;

namespace HypoMux.EngineClient;

public sealed record EngineHelloDto(
    string Engine,
    string EngineVersion,
    string Commit,
    int ProtocolVersion,
    string Transport,
    IReadOnlyList<string> Capabilities,
    IReadOnlyList<string> Modes,
    IReadOnlyDictionary<string, IReadOnlyList<string>> ModeFeatures,
    string Os,
    string Arch,
    int Pid,
    bool Elevated,
    DateTimeOffset StartedAt);

public sealed record EngineStateDto(
    string State,
    long Sequence,
    DateTimeOffset StateChangedAt,
    string? Reason);

public sealed record EngineStatusDto(
    EngineStateDto Engine,
    long HostUptimeMs);

public sealed record EngineEndpointsDto(
    string? Socks,
    string? Http,
    IReadOnlyDictionary<string, string>? Channels);

public sealed record EngineStartResultDto(
    EngineStateDto State,
    string Mode,
    EngineEndpointsDto Endpoints);

public sealed record EngineStopResultDto(
    bool Accepted,
    EngineStateDto State);

public sealed record HealthCheckDto(
    bool Ok,
    string State,
    long HostUptimeMs);

public sealed record TunStatusDto(
    string State,
    int? Pid,
    DateTimeOffset? StartedAt,
    DateTimeOffset? ExitedAt,
    int? ExitCode,
    string? ConfigPath,
    string? LastError);

public sealed record AdapterTelemetryDto(
    string Name,
    string SourceIp,
    int IfIndex,
    string? SourceIpv6,
    int? Ipv6IfIndex,
    int Connections,
    long BytesUp,
    long BytesDown,
    string HealthState,
    int ConsecutiveFailures,
    long HealthSuccesses,
    long HealthFailures,
    DateTimeOffset? LastSuccessAt,
    DateTimeOffset? LastFailureAt,
    DateTimeOffset? CooldownUntil,
    int DomainQuarantines);

public sealed record TelemetryTotalDto(
    int Connections,
    long BytesUp,
    long BytesDown);

public sealed record EngineTelemetryDto(
    DateTimeOffset? StartedAt,
    DateTimeOffset SampledAt,
    IReadOnlyList<AdapterTelemetryDto> Adapters,
    TelemetryTotalDto Total);

public sealed record DiagnosticResultDto(
    string Status,
    double LossRate,
    double AvgLatencyMs,
    double JitterMs,
    int Sent,
    int Received,
    string SrcIp,
    string TargetIp,
    string Note);

public sealed record DnsResolveResultDto(
    string Domain,
    string Adapter,
    string RecordType,
    string Address,
    string Transport,
    string Server,
    bool Cached);

public sealed record TunLifecycleResultDto(
    bool Accepted,
    TunStatusDto Tun);

public sealed record EngineEventDto(
    long Sequence,
    string Name,
    JsonElement Data);

public sealed record EngineLogDto(
    string Component,
    string Message);

public sealed record EngineErrorDto(
    string Code,
    string Message,
    JsonElement? Details);
