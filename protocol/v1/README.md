# HypoMux engine protocol v1

This directory is the language-neutral contract for the Go engine, the
current Python client, and the future C# WPF client.

- `manifest.json` records the advertised methods, events, lifecycle states,
  error codes, and operational semantics.
- `fixtures/messages.json` contains canonical wire-message examples.

The fixture `message` objects are complete JSONL payloads without the trailing
newline. Fixture metadata such as `name`, `kind`, and `method` is not sent on
the wire.

Protocol v1 is additive. Existing fields keep their name, JSON type, and
meaning for the lifetime of v1. New optional fields, methods, events, and
error codes may be added. Removing a field, changing its type or meaning, or
changing lifecycle ordering requires a newly negotiated protocol version.

Timestamps are UTC RFC 3339 values. Durations are integer milliseconds and
byte counters are non-negative integers. Clients must use `engine.hello`
capabilities instead of assuming every v1 engine implements every method.
