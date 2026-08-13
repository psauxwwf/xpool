package xray

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"
)

const ShutdownTimeout = 10 * time.Second

type Process struct {
	cmd  *exec.Cmd
	done <-chan error
}

func Start(binaryPath, configPath string) (*Process, error) {
	cmd := exec.Command(binaryPath, "run", "-config", configPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start Xray: %w", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()
	return &Process{cmd: cmd, done: done}, nil
}

func (p *Process) Done() <-chan error {
	return p.done
}

func (p *Process) Stop() {
	if p == nil || p.cmd == nil || p.cmd.Process == nil || p.cmd.ProcessState != nil {
		return
	}

	_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGINT)
	select {
	case <-p.done:
		return
	case <-time.After(ShutdownTimeout):
	}

	_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL)
	<-p.done
}

func WaitForAPI(ctx context.Context, binaryPath, apiAddress, balancerTag string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, err := BalancerInfo(ctx, binaryPath, apiAddress, balancerTag); err == nil {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func BalancerInfo(ctx context.Context, binaryPath, apiAddress, balancerTag string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, binaryPath, "api", "bi", "--server", apiAddress, "--timeout", "2", "--json", balancerTag)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("xray api bi: %w: %s", err, bytes.TrimSpace(output))
	}
	return output, nil
}

func OverrideBalancer(ctx context.Context, binaryPath, apiAddress, balancerTag, target string) error {
	cmd := exec.CommandContext(ctx, binaryPath, "api", "bo", "--server", apiAddress, "--timeout", "3", "-b", balancerTag, target)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("xray api bo %s: %w: %s", target, err, bytes.TrimSpace(output))
	}
	return nil
}

func MarshalBalancerInfo(raw []byte) (map[string]any, error) {
	var result map[string]any
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	return result, nil
}
