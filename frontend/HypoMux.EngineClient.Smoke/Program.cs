using System.Text.Json;
using HypoMux.EngineClient;

var enginePath = args.Length > 0
    ? Path.GetFullPath(args[0])
    : EngineExecutableResolver.Resolve();
if (enginePath is null)
{
    Console.Error.WriteLine("hypomux-engine.exe was not found");
    return 2;
}

await using var client = new EngineProcessClient(enginePath);
var events = new List<string>();
client.EventReceived += (_, engineEvent) => events.Add(engineEvent.Name);
var hello = await client.StartAsync();
var status = await client.RequestAsync<EngineStatusDto>("engine.status");
var health = await client.RequestAsync<HealthCheckDto>("health.check");

Console.WriteLine(JsonSerializer.Serialize(
    new
    {
        engine = hello.Engine,
        hello.ProtocolVersion,
        hello.Transport,
        hello.Pid,
        state = status.Engine.State,
        health = health.Ok,
        events,
    },
    new JsonSerializerOptions { WriteIndented = true }));
return health.Ok && status.Engine.State == "stopped" ? 0 : 1;
