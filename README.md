# repl-nats-tap

Passive NATS tap for Dialtone REPL traffic.

This tool is intentionally independent from the `dialtone` repo:
- separate git repository
- no shared code imports from Dialtone
- read-only NATS subscriber (does not publish commands)
- never starts an embedded NATS server
- auto-reconnects when upstream NATS goes up/down

## Requirements

- Go 1.24+

## Build

```bash
go build -o repl-nats-tap .
```

## Run

Tap all REPL subjects:

```bash
./repl-nats-tap --upstream nats://127.0.0.1:4222 --subjects 'repl.>'
```

Tap only room traffic:

```bash
./repl-nats-tap --subjects 'repl.room.>'
```

Tap command stream + room stream:

```bash
./repl-nats-tap --subjects 'repl.cmd,repl.room.>'
```

Show raw payloads:

```bash
./repl-nats-tap --raw --subjects 'repl.>'
```

## Behavior

- If NATS is down at startup, client keeps retrying.
- If NATS disconnects later, client auto-reconnects.
- Messages are printed in a REPL-style format for `repl.*` frames.
- Unknown/non-JSON payloads are printed as plain text.

## Why this does not interfere

- Subscriber-only: no command injection, no service startup, no config writes.
- Uses its own NATS client connection and durable reconnect loop.
