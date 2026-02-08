package terraform

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/hashicorp/terraform-exec/tfexec"
	"github.com/plasmash/plasmactl-node/pkg/node"
)

// TerraformManager handles Terraform/OpenTofu operations
type TerraformManager struct {
	workDir     string
	tf          *tfexec.Terraform
	dryRun      bool
	autoApprove bool
}

// ServerOutput represents a provisioned server from Terraform
type ServerOutput struct {
	Hostname   string `json:"hostname"`
	PublicIP   string `json:"public_ip"`
	FailoverIP string `json:"failover_ip"`
	PrivateIP  string `json:"private_ip"`
	PrivateMAC string `json:"private_mac"`
	ServerID   string `json:"server_id"`
	Zone       string `json:"zone"`
	Region     string `json:"region"`
	OfferName  string `json:"offer_name"`
	Chassis    string `json:"chassis"`
	Provider   string `json:"provider"`
}

// ProviderConfig holds provider-agnostic configuration for HCL generation
type ProviderConfig struct {
	EnvName   string
	Specs     []node.ChassisSpec
	Provider  string
	APIToken  string
	Zone      string
	Region    string
	ProjectID string
	Image     string
	SSHKeyID  string
}

// providerTemplates maps provider names to their HCL templates
var providerTemplates = map[string]string{}

// RegisterTemplate registers an HCL template for a provider
func RegisterTemplate(provider, tmpl string) {
	providerTemplates[provider] = tmpl
}

// NewTerraformManager creates a new Terraform manager
func NewTerraformManager(envDir string, dryRun, autoApprove bool) (*TerraformManager, error) {
	workDir := filepath.Join(envDir, ".terraform")

	// Create working directory
	if err := os.MkdirAll(workDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create terraform directory: %w", err)
	}

	// Find tofu or terraform binary
	execPath, err := findTerraformBinary()
	if err != nil {
		return nil, err
	}

	tf, err := tfexec.NewTerraform(workDir, execPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create terraform instance: %w", err)
	}

	return &TerraformManager{
		workDir:     workDir,
		tf:          tf,
		dryRun:      dryRun,
		autoApprove: autoApprove,
	}, nil
}

// findTerraformBinary finds tofu or terraform in PATH
func findTerraformBinary() (string, error) {
	// Prefer OpenTofu
	for _, name := range []string{"tofu", "terraform"} {
		path, err := exec.LookPath(name)
		if err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("neither tofu nor terraform found in PATH")
}

// GenerateHCL generates Terraform HCL for the configured provider
func (m *TerraformManager) GenerateHCL(config ProviderConfig) error {
	mainFile := filepath.Join(m.workDir, "main.tf")

	tmplStr, ok := providerTemplates[config.Provider]
	if !ok {
		return fmt.Errorf("no terraform template registered for provider %q", config.Provider)
	}

	funcMap := template.FuncMap{
		"seq": func(start, count int) []int {
			s := make([]int, count)
			for i := range s {
				s[i] = start + i
			}
			return s
		},
		"add": func(a, b int) int { return a + b },
		"replace": func(old, new, s string) string {
			return strings.ReplaceAll(s, old, new)
		},
		"splitList": func(sep, s string) []string {
			return strings.Split(s, sep)
		},
		"last": func(s []string) string {
			if len(s) == 0 {
				return ""
			}
			return s[len(s)-1]
		},
	}

	tmpl, err := template.New("main.tf").Funcs(funcMap).Parse(tmplStr)
	if err != nil {
		return fmt.Errorf("failed to parse template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, config); err != nil {
		return fmt.Errorf("failed to execute template: %w", err)
	}

	if err := os.WriteFile(mainFile, buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("failed to write main.tf: %w", err)
	}

	return nil
}

// Init initializes Terraform
func (m *TerraformManager) Init(ctx context.Context) error {
	return m.tf.Init(ctx, tfexec.Upgrade(true))
}

// Plan runs terraform plan
func (m *TerraformManager) Plan(ctx context.Context) (bool, error) {
	return m.tf.Plan(ctx)
}

// Apply runs terraform apply
func (m *TerraformManager) Apply(ctx context.Context) error {
	var opts []tfexec.ApplyOption
	if m.autoApprove {
		// terraform-exec always uses -auto-approve
	}
	return m.tf.Apply(ctx, opts...)
}

// Destroy runs terraform destroy
func (m *TerraformManager) Destroy(ctx context.Context) error {
	return m.tf.Destroy(ctx)
}

// GetOutputs retrieves Terraform outputs
func (m *TerraformManager) GetOutputs(ctx context.Context) ([]ServerOutput, error) {
	outputs, err := m.tf.Output(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get terraform outputs: %w", err)
	}

	serversOutput, ok := outputs["servers"]
	if !ok {
		return nil, nil
	}

	var servers map[string]ServerOutput
	if err := json.Unmarshal(serversOutput.Value, &servers); err != nil {
		return nil, fmt.Errorf("failed to unmarshal servers output: %w", err)
	}

	var result []ServerOutput
	for _, s := range servers {
		result = append(result, s)
	}

	return result, nil
}

// GetWorkDir returns the Terraform working directory
func (m *TerraformManager) GetWorkDir() string {
	return m.workDir
}

