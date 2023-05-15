/*
Package exec provides a k6 extension allowing users to execute shell commands from k6 scripts.

The package includes support for custom k6 metrics to track various aspects of command execution
such as duration, stdout/stderr bytes, and failure rate. The metrics are exposed to k6 and will
appear in the summary at the end of a k6 test execution.

The exec package introduces a new global object 'Cmd' in the k6 JavaScript context, which can be
used to construct commands. Each 'Cmd' object has an 'Arg' method for adding command-line arguments, an
'Env' method for setting environment variables, and an 'Exec' method for executing the command and
returning a promise that resolves with the command's result.

The 'Cmd' object's 'Exec' method runs the command in a non-blocking manner and returns a promise, making
it compatible with the k6 event loop.

Command executions are done within the context of the Virtual User (VU) that called the 'Exec' method, and
the command will be interrupted if the VU context is cancelled.

Note: The current implementation of the exec package should be considered experimental and potentially
unsafe. It allows scripts to execute arbitrary commands on the system running k6, which could be a security
risk if k6 is used to run untrusted scripts.

Example usage:

```
import exec from 'k6/x/exec';

	export default async function() {
		let cmd = new exec.Cmd("echo").Arg("Hello, World!");

		const result = await cmd.Exec();

		console.log(result.stdout); // Output: "Hello, World!"
	}

```
*/
package exec

import (
	"errors"
	"io"
	"os/exec"
	"strconv"
	"time"

	"github.com/dop251/goja"
	"go.k6.io/k6/js/common"
	"go.k6.io/k6/js/modules"
	"go.k6.io/k6/metrics"
)

type (
	// RootModule is the global module instance that will create Client
	// instances for each VU.
	RootModule struct{}

	// ModuleInstance represents an instance of the JS module.
	ModuleInstance struct {
		vu modules.VU

		*Command
		Metrics *CustomMetrics
	}
)

// Ensure the interfaces are implemented correctly
var (
	_ modules.Instance = &ModuleInstance{}
	_ modules.Module   = &RootModule{}
)

// New returns a pointer to a new RootModule instance
func New() *RootModule {
	return &RootModule{}
}

// NewModuleInstance implements the modules.Module interface and returns
// a new instance for each VU.
func (*RootModule) NewModuleInstance(vu modules.VU) modules.Instance {
	vu.Runtime().SetFieldNameMapper(goja.TagFieldNameMapper("js", true))

	return &ModuleInstance{
		vu:      vu,
		Command: &Command{vu: vu},
		Metrics: RegisterCustomMetrics(vu.InitEnv().Registry),
	}
}

// Exports implements the modules.Instance interface and returns
// the exports of the JS module.
func (mi *ModuleInstance) Exports() modules.Exports {
	return modules.Exports{Named: map[string]interface{}{
		"Cmd": mi.NewCmd,
	}}
}

// CustomMetrics are the custom k6 metrics used by xk6-browser.
type CustomMetrics struct {
	ExecCommandDuration         *metrics.Metric
	ExecCommandsTotal           *metrics.Metric
	ExecCommandStdoutBytesTotal *metrics.Metric
	ExecCommandStderrBytesTotal *metrics.Metric
	ExecCommandFailedRate       *metrics.Metric
}

// RegisterCustomMetrics creates and registers our custom metrics with the k6
// VU Registry and returns our internal struct pointer.
func RegisterCustomMetrics(registry *metrics.Registry) *CustomMetrics {
	return &CustomMetrics{
		ExecCommandsTotal: registry.MustNewMetric(
			"exec_commands_total",
			metrics.Counter,
			metrics.ValueType(metrics.Counter),
		),
		ExecCommandDuration: registry.MustNewMetric(
			"exec_command_duration",
			metrics.Trend,
			metrics.Time,
		),
		ExecCommandStdoutBytesTotal: registry.MustNewMetric(
			"exec_command_stdout_bytes",
			metrics.Trend,
			metrics.Data,
		),
		ExecCommandStderrBytesTotal: registry.MustNewMetric(
			"exec_command_stderr_bytes",
			metrics.Trend,
			metrics.Data,
		),
		ExecCommandFailedRate: registry.MustNewMetric(
			"exec_command_failed_rate",
			metrics.Rate,
		),
	}
}

// NewCmd is the JS constructor for the Cmd object.
func (mi *ModuleInstance) NewCmd(call goja.ConstructorCall) *goja.Object {
	rt := mi.vu.Runtime()

	var name string
	err := rt.ExportTo(call.Argument(0), &name)
	if err != nil {
		common.Throw(rt, err)
	}

	command := &Command{
		Name:    name,
		args:    make([]string, 0),
		env:     make(map[string]string),
		vu:      mi.vu,
		metrics: mi.Metrics,
	}

	return rt.ToValue(command).ToObject(rt)
}

// Command represents a command to be executed.
type Command struct {
	Name string

	args []string
	env  map[string]string

	vu      modules.VU
	metrics *CustomMetrics
}

// Arg adds an argument to the command.
func (c Command) Arg(arg string) Command {
	c.args = append(c.args, arg)
	return c
}

// Env sets an environment variable for the command.
func (c Command) Env(key, value string) Command {
	c.env[key] = value
	return c
}

// Exec runs the command and returns a promise that will be resolved when the command finishes.
// FIXME: this is probably very unsafe.
func (c *Command) Exec() *goja.Promise {
	vuContext := c.vu.Context()
	vuState := c.vu.State()

	promise, resolve, reject := makeHandledPromise(c.vu)

	cmdPath, err := exec.LookPath(c.Name)
	if errors.Is(err, exec.ErrDot) {
		err = nil
	}

	if err != nil {
		reject(err)
		return promise
	}

	environ := make([]string, 0, len(c.env))
	for k, v := range c.env {
		environ = append(environ, k+"="+v)
	}

	cmd := exec.CommandContext(vuContext, cmdPath, c.args...)
	cmd.Env = append(cmd.Environ(), environ...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		reject(err)
		return promise
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		reject(err)
		return promise
	}

	start := time.Now()
	if err := cmd.Start(); err != nil {
		reject(err)
		return promise
	}

	go func() {
		stdoutBytes, err := io.ReadAll(stdout)
		if err != nil {
			reject(err)
			return
		}

		stderrBytes, err := io.ReadAll(stderr)
		if err != nil {
			reject(err)
			return
		}

		var exitCode int
		if err := cmd.Wait(); err != nil {
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				exitCode = exitErr.ExitCode()
			}
		}

		end := time.Now()
		duration := end.Sub(start)

		// FIXME: still somewhat confused as to how rate metrics
		// function when used as a "boolean" metric.
		// I would imagine the reverse logic would produce the output
		// I would naively expect, but it does not.
		var failed float64
		if exitCode == 0 {
			failed = 1
		}

		tags := vuState.Tags.GetCurrentValues().Tags
		tags = tags.With("executable", c.Name)
		tags = tags.With("exit_code", strconv.Itoa(exitCode))

		metrics.PushIfNotDone(vuContext, vuState.Samples, metrics.ConnectedSamples{
			Samples: []metrics.Sample{
				{
					TimeSeries: metrics.TimeSeries{Metric: c.metrics.ExecCommandDuration, Tags: tags},
					Value:      float64(duration.Milliseconds()),
					Time:       end,
				},
				{
					TimeSeries: metrics.TimeSeries{Metric: c.metrics.ExecCommandsTotal, Tags: tags},
					Value:      1,
					Time:       end,
				},
				{
					TimeSeries: metrics.TimeSeries{Metric: c.metrics.ExecCommandStdoutBytesTotal, Tags: tags},
					Value:      float64(len(stdoutBytes)),
					Time:       end,
				},
				{
					TimeSeries: metrics.TimeSeries{Metric: c.metrics.ExecCommandStderrBytesTotal, Tags: tags},
					Value:      float64(len(stderrBytes)),
					Time:       end,
				},
				{
					TimeSeries: metrics.TimeSeries{Metric: c.metrics.ExecCommandFailedRate, Tags: tags},
					Value:      failed,
					Time:       end,
				},
			},
		})

		resolve(CommandResult{ExitCode: exitCode, Stdout: string(stdoutBytes), Stderr: string(stderrBytes)})
	}()

	return promise
}

// CommandResult holds the result of a command execution.
type CommandResult struct {
	ExitCode int    `js:"exitCode"`
	Stdout   string `js:"stdout"`
	Stderr   string `js:"stderr"`
}

// makeHandledPromise will create a promise and return its resolve and reject methods,
// wrapped in such a way that it will block the eventloop from exiting before they are
// called even if the promise isn't resolved by the time the current script ends executing.
func makeHandledPromise(vu modules.VU) (*goja.Promise, func(interface{}), func(interface{})) {
	runtime := vu.Runtime()
	callback := vu.RegisterCallback()
	p, resolve, reject := runtime.NewPromise()

	return p, func(i interface{}) {
			// more stuff
			callback(func() error {
				resolve(i)
				return nil
			})
		}, func(i interface{}) {
			// more stuff
			callback(func() error {
				reject(i)
				return nil
			})
		}
}
