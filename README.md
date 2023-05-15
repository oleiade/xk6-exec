# k6 Extension - Exec

This extension for k6 allows you to execute shell commands from your k6 scripts.
It exposes a new global object `Cmd` in the k6 JavaScript context, which can be used to construct commands.

## Installation

First, you need to install [xk6](https://github.com/k6io/xk6) and then build the k6 binary with the exec extension.

To install xk6, run:

```bash
go install github.com/grafana/xk6/cmd/xk6@latest
```

Then build the k6 binary:

```bash
xk6 build --with github.com/oleiade/xk6-exec@latest
```

This will result in a `k6` binary in the current directory.

## Usage

After you've built the `k6` binary, you can use it to run your scripts that use the `exec` extension. Here's a simple example:

```javascript
import { check } from "k6";
import { Cmd } from "k6/x/cmd";

export default async function () {
  // Produce the command you wish to run
  // using the composable API.
  const cmd = new Cmd("motus")
    .arg("memorable")
    .arg("--words")
    .arg("12")
    .env("NO_COLOR", "true");

  // Execute the command and wait for it to finish.
  const result = await cmd.exec();

  // Check the result of the command.
  check(result, {
    "exit code is 0": (r) => r.exitCode === 0,
    "stdout contains 12 words": (r) => r.stdout.split(" ").length === 12,
  });
}
```

In the above script, we're creating a new `Cmd` object with the command `motus`. We add arguments to the command using the `Arg` method. We add environment variables using the `Env` method. Then we execute the command with the `Exec` method, which returns a promise that resolves with the command's result.

## Metrics

The exec extension also provides custom k6 metrics:

- `exec_command_duration`: The duration of the command execution.
- `exec_commands_total`: The total number of executed commands.
- `exec_command_stdout_bytes`: The total number of bytes written to stdout by the command.
- `exec_command_stderr_bytes`: The total number of bytes written to stderr by the command.
- `exec_command_failed_rate`: The rate of command executions that failed.

These metrics are exposed to k6 and will appear in the summary at the end of a k6 test execution.

## Caution

This extension is potentially unsafe as it allows scripts to execute arbitrary commands on the system running k6. Use it responsibly and avoid running untrusted scripts.
