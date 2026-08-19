package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/iAghaTraker/InfraPilot/internal/firewall"
	"github.com/iAghaTraker/InfraPilot/internal/identity"
	"gopkg.in/yaml.v3"
)

func runWebSetup(ctx context.Context, env Env, out IO) error {
	fmt.Fprintln(out.Out, "InfraPilot Web Setup")
	if out.In == nil {
		return fmt.Errorf("interactive confirmation is required")
	}
	identityOK := false
	if _, err := identity.Load(identityDir(env)); err == nil {
		identityOK = true
	}
	fmt.Fprintf(out.Out, "Device identity: %s\n", yesNo(identityOK))
	fw := firewall.Detect(ctx, 8090)
	fmt.Fprintf(out.Out, "Firewall: %s (%s)\n", fw.Detected, fw.Status)
	public := detectPublicIP(ctx)
	if public == "" {
		public = "unavailable"
	}
	fmt.Fprintf(out.Out, "Public address: %s\n", public)
	fmt.Fprint(out.Out, "Expose Web Panel to network? (y/N): ")
	line, _ := bufio.NewReader(out.In).ReadString('\n')
	bind := "127.0.0.1"
	if strings.EqualFold(strings.TrimSpace(line), "y") || strings.EqualFold(strings.TrimSpace(line), "yes") {
		bind = "0.0.0.0"
		if fw.Detected != "" && fw.Status == "blocked" {
			fmt.Fprint(out.Out, "Port 8090 is blocked by firewall. Allow access? (y/N): ")
			line, _ = bufio.NewReader(out.In).ReadString('\n')
			if strings.EqualFold(strings.TrimSpace(line), "y") || strings.EqualFold(strings.TrimSpace(line), "yes") {
				if fw.Detected == "ufw" {
					_ = execFirewall(ctx, "ufw", "allow", "8090/tcp")
				} else if fw.Detected == "firewalld" {
					_ = execFirewall(ctx, "firewall-cmd", "--permanent", "--add-port=8090/tcp")
					_ = execFirewall(ctx, "firewall-cmd", "--reload")
				}
			}
		}
	}
	if err := saveBind(env.Paths.ConfigFile, bind); err != nil {
		return err
	}
	_ = newWebManager().Restart(ctx)
	fmt.Fprintln(out.Out, "\nInfraPilot Web Setup Complete\n\nAuthentication: Enabled\nSecurity: Ed25519 challenge-response")
	if bind == "0.0.0.0" && public != "unavailable" {
		fmt.Fprintf(out.Out, "Panel URL: http://%s:8090\n", public)
	} else {
		fmt.Fprintln(out.Out, "Panel URL: http://127.0.0.1:8090")
	}
	return nil
}

func yesNo(v bool) string {
	if v {
		return "connected"
	}
	return "not found"
}
func execFirewall(ctx context.Context, name string, args ...string) error {
	return firewallCommand(ctx, name, args...)
}

var firewallCommand = func(ctx context.Context, name string, args ...string) error {
	return exec.CommandContext(ctx, name, args...).Run()
}

func detectPublicIP(ctx context.Context) string {
	client := &http.Client{Timeout: 3 * time.Second}
	for _, endpoint := range []string{"https://api.ipify.org", "https://ifconfig.me/ip"} {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 64))
		_ = resp.Body.Close()
		ip := strings.TrimSpace(string(b))
		if net.ParseIP(ip) != nil {
			return ip
		}
	}
	return ""
}

func publicIP(ctx context.Context) string {
	ip := detectPublicIP(ctx)
	if ip == "" {
		return "unavailable"
	}
	return ip
}

func saveBind(path, bind string) error {
	data, err := os.ReadFile(path)
	root := map[string]any{}
	if err == nil && len(strings.TrimSpace(string(data))) > 0 {
		if err := yaml.Unmarshal(data, &root); err != nil {
			return err
		}
	}
	web, _ := root["web"].(map[string]any)
	if web == nil {
		web = map[string]any{}
	}
	web["bind_address"] = bind
	root["web"] = web
	encoded, err := yaml.Marshal(root)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	return os.WriteFile(path, encoded, 0o640)
}
