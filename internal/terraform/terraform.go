package terraform

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
	Hostname   string
	PublicIP   string
	PrivateMAC string
	ServerID   string
	Zone       string
	OfferName  string
	Chassis    string
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

// GenerateHCL generates Terraform HCL for Scaleway Dedibox
func (m *TerraformManager) GenerateHCL(envName string, specs []node.ChassisSpec, apiToken string) error {
	mainFile := filepath.Join(m.workDir, "main.tf")

	data := struct {
		EnvName  string
		APIToken string
		Specs    []node.ChassisSpec
	}{
		EnvName:  envName,
		APIToken: apiToken,
		Specs:    specs,
	}

	tmpl, err := template.New("main.tf").Parse(scalewayDediboxTemplate)
	if err != nil {
		return fmt.Errorf("failed to parse template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
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

// Scaleway Dedibox Terraform template
const scalewayDediboxTemplate = `
terraform {
  required_providers {
    scaleway = {
      source  = "scaleway/scaleway"
      version = ">= 2.50.0"
    }
  }
}

provider "scaleway" {
  # API token from environment or variable
  secret_key = var.api_token
}

variable "api_token" {
  description = "Scaleway API token"
  type        = string
  sensitive   = true
  default     = "{{ .APIToken }}"
}

variable "project_id" {
  description = "Scaleway project ID"
  type        = string
  default     = ""
}

{{- range $i, $spec := .Specs }}

# Data source for offer: {{ $spec.OfferType }}
data "scaleway_dedibox_offer" "offer_{{ $i }}" {
  name = "{{ $spec.OfferType }}"
}

{{- range $j := seq 0 $spec.Count }}

resource "scaleway_dedibox_server" "{{ $.EnvName | replace "-" "_" }}_{{ $spec.Chassis | replace "." "_" }}_{{ $j }}" {
  offer_id   = data.scaleway_dedibox_offer.offer_{{ $i }}.offer_id
  project_id = var.project_id != "" ? var.project_id : null
  hostname   = "{{ $.EnvName }}-{{ $spec.Chassis | splitList "." | last }}-{{ printf "%03d" (add $j 1) }}"

  tags = [
    "env:{{ $.EnvName }}",
    "chassis:{{ $spec.Chassis }}",
    "managed-by:plasmactl"
  ]
}
{{- end }}
{{- end }}

output "servers" {
  description = "Provisioned servers"
  value = {
{{- range $i, $spec := .Specs }}
{{- range $j := seq 0 $spec.Count }}
    "{{ $.EnvName }}-{{ $spec.Chassis | splitList "." | last }}-{{ printf "%03d" (add $j 1) }}" = {
      hostname    = scaleway_dedibox_server.{{ $.EnvName | replace "-" "_" }}_{{ $spec.Chassis | replace "." "_" }}_{{ $j }}.hostname
      public_ip   = scaleway_dedibox_server.{{ $.EnvName | replace "-" "_" }}_{{ $spec.Chassis | replace "." "_" }}_{{ $j }}.public_ipv4
      private_mac = scaleway_dedibox_server.{{ $.EnvName | replace "-" "_" }}_{{ $spec.Chassis | replace "." "_" }}_{{ $j }}.interfaces[0].mac_address
      server_id   = scaleway_dedibox_server.{{ $.EnvName | replace "-" "_" }}_{{ $spec.Chassis | replace "." "_" }}_{{ $j }}.id
      zone        = scaleway_dedibox_server.{{ $.EnvName | replace "-" "_" }}_{{ $spec.Chassis | replace "." "_" }}_{{ $j }}.zone
      offer_name  = "{{ $spec.OfferType }}"
      chassis     = "{{ $spec.Chassis }}"
    }
{{- end }}
{{- end }}
  }
}
`
