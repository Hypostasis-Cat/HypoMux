namespace HypoMux.EngineClient;

public class EngineClientException : Exception
{
    public EngineClientException(string message)
        : base(message)
    {
    }

    public EngineClientException(string message, Exception innerException)
        : base(message, innerException)
    {
    }
}

public sealed class EngineProtocolException : EngineClientException
{
    public EngineProtocolException(string message)
        : base(message)
    {
    }

    public EngineProtocolException(string message, Exception innerException)
        : base(message, innerException)
    {
    }
}

public sealed class EngineRemoteException : EngineClientException
{
    public EngineRemoteException(
        string code,
        string message,
        string requestId,
        System.Text.Json.JsonElement? details)
        : base($"{code}: {message}")
    {
        Code = code;
        RequestId = requestId;
        Details = details;
    }

    public string Code { get; }

    public string RequestId { get; }

    public System.Text.Json.JsonElement? Details { get; }
}
