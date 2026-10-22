// Command selfci runs ordinary tests through Receipt in a fixed, clean CI clone.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	checkout     = "/tmp/receipt-self-checkout"
	home         = "/tmp/receipt-self-home"
	clientBinary = "/tmp/receipt-self-client"
	remote       = "https://receipt.sidsharma.dev"
)

func testEnvironment(goDirectory, image string) ([]string, error) {
	if image == "" {
		return nil, fmt.Errorf("runner image version is required for safe cache separation")
	}
	return []string{"PATH=" + goDirectory + ":/usr/bin:/bin", "HOME=" + home, "GOCACHE=" + home + "/build", "GOPATH=" + home + "/go", "CGO_ENABLED=0", "GOTOOLCHAIN=local", "LANG=C", "TZ=UTC", "RECEIPT_RUNNER_IMAGE=" + image}, nil
}

func remoteReady(ctx context.Context, base, token string, transport http.RoundTripper) bool {
	if token == "" {
		return false
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/v1/identity", nil)
	if err != nil || request.URL.Scheme != "https" {
		return false
	}
	request.Header.Set("Authorization", "Bearer "+token)
	client := &http.Client{Timeout: 10 * time.Second, Transport: transport, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return false
	}
	// Read at most one small identity payload; malformed/unavailable cache falls back.
	data, readErr := io.ReadAll(io.LimitReader(response.Body, 4097))
	closeErr := response.Body.Close()
	if readErr != nil || closeErr != nil || len(data) > 4096 || response.StatusCode != http.StatusOK {
		return false
	}
	var identity struct {
		Subject string `json:"subject"`
	}
	return json.Unmarshal(data, &identity) == nil && identity.Subject == "receipt-self-ci"
}

func runCommand(env []string, args ...string) error {
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir, cmd.Env = checkout, env
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w", args[0], err)
	}
	return nil
}

func prepare() ([]string, error) {
	for _, path := range []string{checkout, home} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			return nil, fmt.Errorf("refusing to reuse CI workspace %s", path)
		}
	}
	goPath, err := exec.LookPath("go")
	if err != nil {
		return nil, fmt.Errorf("find Go: %w", err)
	}
	goPath, err = filepath.EvalSymlinks(goPath)
	if err != nil {
		return nil, fmt.Errorf("resolve Go: %w", err)
	}
	env, err := testEnvironment(filepath.Dir(goPath), os.Getenv("ImageVersion"))
	if err != nil {
		return nil, err
	}
	if err := exec.Command("git", "clone", "--quiet", "--no-hardlinks", ".", checkout).Run(); err != nil {
		return nil, fmt.Errorf("clone committed source: %w", err)
	}
	if err := os.Mkdir(home, 0o700); err != nil {
		return nil, fmt.Errorf("create CI home: %w", err)
	}
	if err := runCommand(env, "go", "mod", "download"); err != nil {
		return nil, err
	}
	if err := runCommand(env, "go", "mod", "verify"); err != nil {
		return nil, err
	}
	env = append(env, "GOPROXY=off")
	if err := declareInputs(env); err != nil {
		return nil, err
	}
	return env, nil
}

func declareInputs(env []string) error {
	cmd := exec.Command("go", "list", "-m", "all")
	cmd.Dir, cmd.Env = checkout, env
	modules, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("list CI modules: %w", err)
	}
	if strings.TrimSpace(string(modules)) != "github.com/sidsharma96/receipt\ngithub.com/lib/pq v1.10.9" {
		return fmt.Errorf("update declared CI module inputs for changed dependencies")
	}
	config := struct {
		ExtraInputs []string `json:"extra_inputs"`
	}{[]string{home + "/go/pkg/mod/github.com/lib/pq@v1.10.9", "/usr/bin/git", "/usr/bin/dash", clientBinary}}
	file, err := os.OpenFile(filepath.Join(checkout, ".receipt.json"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create CI declarations; review existing repository configuration: %w", err)
	}
	return errors.Join(json.NewEncoder(file).Encode(config), file.Close())
}

func run() error {
	env, err := prepare()
	if err != nil {
		return err
	}
	token := os.Getenv("RECEIPT_SELF_CI_TOKEN")
	args := []string{clientBinary, "run", "--cache-dir", home + "/receipt"}
	if remoteReady(context.Background(), remote, token, nil) {
		env = append(env, "RECEIPT_TOKEN="+token)
		args = append(args, "--remote", remote)
	} else {
		log.Print("HTTPS cache unavailable or credentials absent; running local tests.")
	}
	args = append(args, "--", "go", "test", "-count=1", "./...")
	started := time.Now()
	err = runCommand(env, args...)
	log.Printf("Receipt invocation: %.2fs", time.Since(started).Seconds())
	return err
}

func main() {
	log.SetFlags(0)
	if err := run(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			os.Exit(exit.ExitCode())
		}
		log.Fatal(err)
	}
}
