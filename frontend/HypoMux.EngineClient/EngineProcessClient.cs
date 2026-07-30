using System.Collections.Concurrent;
using System.Diagnostics;
using System.Text;
using System.Text.Json;

namespace HypoMux.EngineClient;

public sealed class EngineProcessClient : IAsyncDisposable
{
    public const int ProtocolVersion = 1;
    public const int MaxMessageBytes = 1024 * 1024;

    private static readonly JsonSerializerOptions SerializerOptions = new()
    {
        PropertyNamingPolicy = JsonNamingPolicy.SnakeCaseLower,
        DictionaryKeyPolicy = JsonNamingPolicy.SnakeCaseLower,
        PropertyNameCaseInsensitive = true,
    };

    private readonly string _executablePath;
    private readonly ConcurrentDictionary<string, TaskCompletionSource<JsonElement>> _pending = new();
    private readonly SemaphoreSlim _writeGate = new(1, 1);
    private readonly SemaphoreSlim _lifecycleGate = new(1, 1);
    private Process? _process;
    private CancellationTokenSource? _lifetime;
    private Task? _stdoutTask;
    private Task? _stderrTask;
    private long _requestSequence;
    private long _lastEventSequence;
    private int _stopping;
    private int _disconnectSignaled;

    public EngineProcessClient(string executablePath)
    {
        ArgumentException.ThrowIfNullOrWhiteSpace(executablePath);
        _executablePath = Path.GetFullPath(executablePath);
    }

    public event EventHandler<EngineEventDto>? EventReceived;

    public event EventHandler<string>? StandardErrorReceived;

    public event EventHandler<Exception?>? Disconnected;

    public EngineHelloDto? Hello { get; private set; }

    public bool IsRunning => _process is { HasExited: false };

    public async Task<EngineHelloDto> StartAsync(
        CancellationToken cancellationToken = default)
    {
        await _lifecycleGate.WaitAsync(cancellationToken).ConfigureAwait(false);
        try
        {
            if (IsRunning && Hello is not null)
            {
                return Hello;
            }

            if (!File.Exists(_executablePath))
            {
                throw new FileNotFoundException(
                    "HypoMux Go engine was not found.",
                    _executablePath);
            }

            await ResetPreviousProcessAsync().ConfigureAwait(false);
            Interlocked.Exchange(ref _stopping, 0);
            Interlocked.Exchange(ref _disconnectSignaled, 0);
            Interlocked.Exchange(ref _lastEventSequence, 0);
            _pending.Clear();

            var startInfo = new ProcessStartInfo
            {
                FileName = _executablePath,
                WorkingDirectory = Path.GetDirectoryName(_executablePath)!,
                UseShellExecute = false,
                CreateNoWindow = true,
                RedirectStandardInput = true,
                RedirectStandardOutput = true,
                RedirectStandardError = true,
                StandardInputEncoding = new UTF8Encoding(false),
                StandardOutputEncoding = new UTF8Encoding(false),
                StandardErrorEncoding = new UTF8Encoding(false),
            };

            _process = new Process
            {
                StartInfo = startInfo,
                EnableRaisingEvents = true,
            };
            if (!_process.Start())
            {
                throw new EngineClientException(
                    "Windows did not start the HypoMux engine process.");
            }

            _lifetime = new CancellationTokenSource();
            _stdoutTask = ReadStandardOutputAsync(_process, _lifetime.Token);
            _stderrTask = ReadStandardErrorAsync(_process, _lifetime.Token);

            var hello = await RequestAsync<EngineHelloDto>(
                "engine.hello",
                parameters: null,
                timeout: TimeSpan.FromSeconds(5),
                cancellationToken).ConfigureAwait(false);
            ValidateHello(hello);
            Hello = hello;
            return hello;
        }
        catch
        {
            await StopProcessAsync(graceful: false).ConfigureAwait(false);
            throw;
        }
        finally
        {
            _lifecycleGate.Release();
        }
    }

    public async Task<T> RequestAsync<T>(
        string method,
        object? parameters = null,
        TimeSpan? timeout = null,
        CancellationToken cancellationToken = default)
    {
        var result = await RequestAsync(
            method,
            parameters,
            timeout,
            cancellationToken).ConfigureAwait(false);
        try
        {
            return result.Deserialize<T>(SerializerOptions)
                ?? throw new EngineProtocolException(
                    $"Engine response for {method} was empty.");
        }
        catch (JsonException exception)
        {
            throw new EngineProtocolException(
                $"Engine response for {method} did not match its DTO.",
                exception);
        }
    }

    public async Task<JsonElement> RequestAsync(
        string method,
        object? parameters = null,
        TimeSpan? timeout = null,
        CancellationToken cancellationToken = default)
    {
        ArgumentException.ThrowIfNullOrWhiteSpace(method);
        var process = _process;
        if (process is null || process.HasExited)
        {
            throw new EngineClientException("The HypoMux engine is not running.");
        }

        if (Volatile.Read(ref _stopping) != 0 && method != "host.shutdown")
        {
            throw new EngineClientException("The HypoMux engine is stopping.");
        }

        var requestId = $"cs-{Interlocked.Increment(ref _requestSequence)}";
        var request = new Dictionary<string, object?>
        {
            ["protocol"] = ProtocolVersion,
            ["id"] = requestId,
            ["method"] = method,
        };
        if (parameters is not null)
        {
            request["params"] = parameters;
        }

        var line = JsonSerializer.Serialize(request, SerializerOptions);
        if (Encoding.UTF8.GetByteCount(line) > MaxMessageBytes)
        {
            throw new EngineProtocolException(
                "Engine request exceeds the protocol message size limit.");
        }

        var completion = new TaskCompletionSource<JsonElement>(
            TaskCreationOptions.RunContinuationsAsynchronously);
        if (!_pending.TryAdd(requestId, completion))
        {
            throw new EngineClientException(
                "Could not allocate a unique engine request identifier.");
        }

        try
        {
            await _writeGate.WaitAsync(cancellationToken).ConfigureAwait(false);
            try
            {
                if (process.HasExited)
                {
                    throw new EngineClientException(
                        "The engine exited before the request was sent.");
                }

                await process.StandardInput.WriteLineAsync(line)
                    .ConfigureAwait(false);
                await process.StandardInput.FlushAsync(cancellationToken)
                    .ConfigureAwait(false);
            }
            finally
            {
                _writeGate.Release();
            }

            using var deadline = CancellationTokenSource.CreateLinkedTokenSource(
                cancellationToken);
            deadline.CancelAfter(timeout ?? TimeSpan.FromSeconds(3));
            try
            {
                return await completion.Task
                    .WaitAsync(deadline.Token)
                    .ConfigureAwait(false);
            }
            catch (OperationCanceledException)
                when (!cancellationToken.IsCancellationRequested)
            {
                throw new TimeoutException(
                    $"Engine request '{method}' timed out.");
            }
        }
        finally
        {
            _pending.TryRemove(requestId, out _);
        }
    }

    public async Task StopAsync()
    {
        await _lifecycleGate.WaitAsync().ConfigureAwait(false);
        try
        {
            await StopProcessAsync(graceful: true).ConfigureAwait(false);
        }
        finally
        {
            _lifecycleGate.Release();
        }
    }

    public async ValueTask DisposeAsync()
    {
        await StopAsync().ConfigureAwait(false);
        _writeGate.Dispose();
        _lifecycleGate.Dispose();
    }

    private async Task ReadStandardOutputAsync(
        Process process,
        CancellationToken cancellationToken)
    {
        Exception? disconnectError = null;
        try
        {
            while (!cancellationToken.IsCancellationRequested)
            {
                var line = await process.StandardOutput
                    .ReadLineAsync(cancellationToken)
                    .ConfigureAwait(false);
                if (line is null)
                {
                    break;
                }

                if (Encoding.UTF8.GetByteCount(line) > MaxMessageBytes)
                {
                    throw new EngineProtocolException(
                        "Engine output exceeds the protocol message size limit.");
                }

                DispatchMessage(line);
            }

            if (Volatile.Read(ref _stopping) == 0)
            {
                disconnectError = new EngineClientException(
                    "The HypoMux engine exited unexpectedly.");
            }
        }
        catch (OperationCanceledException)
            when (cancellationToken.IsCancellationRequested)
        {
        }
        catch (Exception exception)
        {
            disconnectError = exception;
            TryKill(process);
        }
        finally
        {
            SignalDisconnected(disconnectError);
        }
    }

    private async Task ReadStandardErrorAsync(
        Process process,
        CancellationToken cancellationToken)
    {
        try
        {
            while (!cancellationToken.IsCancellationRequested)
            {
                var line = await process.StandardError
                    .ReadLineAsync(cancellationToken)
                    .ConfigureAwait(false);
                if (line is null)
                {
                    return;
                }

                if (!string.IsNullOrWhiteSpace(line))
                {
                    SafeRaise(StandardErrorReceived, line);
                }
            }
        }
        catch (OperationCanceledException)
            when (cancellationToken.IsCancellationRequested)
        {
        }
        catch (ObjectDisposedException)
        {
        }
    }

    private void DispatchMessage(string line)
    {
        JsonDocument document;
        try
        {
            document = JsonDocument.Parse(line);
        }
        catch (JsonException exception)
        {
            throw new EngineProtocolException(
                "Engine produced invalid JSON.",
                exception);
        }

        using (document)
        {
            var root = document.RootElement;
            if (root.ValueKind != JsonValueKind.Object)
            {
                throw new EngineProtocolException(
                    "Engine message must be a JSON object.");
            }

            if (!root.TryGetProperty("protocol", out var protocol)
                || protocol.GetInt32() != ProtocolVersion)
            {
                throw new EngineProtocolException(
                    "Engine message uses an unsupported protocol version.");
            }

            if (root.TryGetProperty("id", out var idElement))
            {
                var requestId = idElement.GetString() ?? string.Empty;
                if (!_pending.TryRemove(requestId, out var completion))
                {
                    return;
                }

                if (root.TryGetProperty("result", out var result))
                {
                    completion.TrySetResult(result.Clone());
                    return;
                }

                if (root.TryGetProperty("error", out var error))
                {
                    completion.TrySetException(CreateRemoteException(
                        requestId,
                        error));
                    return;
                }

                completion.TrySetException(
                    new EngineProtocolException(
                        "Engine response contains neither result nor error."));
                return;
            }

            if (root.TryGetProperty("event", out var eventElement))
            {
                var name = eventElement.GetString();
                if (string.IsNullOrWhiteSpace(name)
                    || !root.TryGetProperty("sequence", out var sequenceElement)
                    || !sequenceElement.TryGetInt64(out var sequence)
                    || sequence <= 0)
                {
                    throw new EngineProtocolException(
                        "Engine event has invalid name or sequence.");
                }

                var previous = Interlocked.Read(ref _lastEventSequence);
                if (sequence <= previous)
                {
                    throw new EngineProtocolException(
                        $"Engine event sequence {sequence} is not newer than {previous}.");
                }

                Interlocked.Exchange(ref _lastEventSequence, sequence);
                var data = root.TryGetProperty("data", out var dataElement)
                    ? dataElement.Clone()
                    : JsonSerializer.SerializeToElement(
                        new Dictionary<string, object?>());
                SafeRaise(
                    EventReceived,
                    new EngineEventDto(sequence, name, data));
                return;
            }

            throw new EngineProtocolException(
                "Engine message is neither a response nor an event.");
        }
    }

    private static EngineRemoteException CreateRemoteException(
        string requestId,
        JsonElement error)
    {
        if (error.ValueKind != JsonValueKind.Object)
        {
            return new EngineRemoteException(
                "invalid_error",
                "Engine returned a malformed error object.",
                requestId,
                details: null);
        }

        var code = error.TryGetProperty("code", out var codeElement)
            ? codeElement.GetString()
            : null;
        var message = error.TryGetProperty("message", out var messageElement)
            ? messageElement.GetString()
            : null;
        JsonElement? details = error.TryGetProperty("details", out var detailsElement)
            ? detailsElement.Clone()
            : null;
        return new EngineRemoteException(
            code ?? "engine_error",
            message ?? "The engine returned an unspecified error.",
            requestId,
            details);
    }

    private static void ValidateHello(EngineHelloDto hello)
    {
        if (hello.ProtocolVersion != ProtocolVersion
            || !string.Equals(
                hello.Transport,
                "stdio-jsonl",
                StringComparison.Ordinal)
            || !hello.Capabilities.Contains(
                "host.shutdown",
                StringComparer.Ordinal))
        {
            throw new EngineProtocolException(
                "Engine handshake is incompatible with the WPF client.");
        }
    }

    private async Task StopProcessAsync(bool graceful)
    {
        var process = _process;
        if (process is null)
        {
            return;
        }

        Interlocked.Exchange(ref _stopping, 1);
        if (!process.HasExited && graceful)
        {
            try
            {
                await RequestAsync(
                    "host.shutdown",
                    parameters: null,
                    timeout: TimeSpan.FromSeconds(2))
                    .ConfigureAwait(false);
            }
            catch (Exception)
            {
                // Bounded process-tree termination below is the recovery path.
            }
        }

        if (!process.HasExited)
        {
            using var exitDeadline = new CancellationTokenSource(
                TimeSpan.FromSeconds(2));
            try
            {
                await process.WaitForExitAsync(exitDeadline.Token)
                    .ConfigureAwait(false);
            }
            catch (OperationCanceledException)
            {
                TryKill(process);
            }
        }

        _lifetime?.Cancel();
        await AwaitReaderAsync(_stdoutTask).ConfigureAwait(false);
        await AwaitReaderAsync(_stderrTask).ConfigureAwait(false);
        SignalDisconnected(error: null);
        process.Dispose();
        _lifetime?.Dispose();
        _process = null;
        _lifetime = null;
        _stdoutTask = null;
        _stderrTask = null;
        Hello = null;
    }

    private async Task ResetPreviousProcessAsync()
    {
        if (_process is not null)
        {
            await StopProcessAsync(graceful: false).ConfigureAwait(false);
        }
    }

    private void SignalDisconnected(Exception? error)
    {
        if (Interlocked.Exchange(ref _disconnectSignaled, 1) != 0)
        {
            return;
        }

        var pendingError = error
            ?? new EngineClientException("The engine connection closed.");
        foreach (var item in _pending.ToArray())
        {
            if (_pending.TryRemove(item.Key, out var completion))
            {
                completion.TrySetException(pendingError);
            }
        }

        SafeRaise(Disconnected, error);
    }

    private static async Task AwaitReaderAsync(Task? task)
    {
        if (task is null)
        {
            return;
        }

        try
        {
            await task.WaitAsync(TimeSpan.FromSeconds(1)).ConfigureAwait(false);
        }
        catch (Exception)
        {
        }
    }

    private static void TryKill(Process process)
    {
        try
        {
            if (!process.HasExited)
            {
                process.Kill(entireProcessTree: true);
            }
        }
        catch (InvalidOperationException)
        {
        }
        catch (System.ComponentModel.Win32Exception)
        {
        }
    }

    private void SafeRaise<T>(EventHandler<T>? handler, T value)
    {
        if (handler is null)
        {
            return;
        }

        try
        {
            handler(this, value);
        }
        catch (Exception)
        {
        }
    }
}
