package terraform

func init() {
	RegisterTemplate("hetzner", hetznerCloudTemplate)
}

const hetznerCloudTemplate = `
terraform {
  required_providers {
    hcloud = {
      source  = "hetznercloud/hcloud"
      version = ">= 1.45.0"
    }
  }
}

provider "hcloud" {
  token = var.api_token
}

variable "api_token" {
  description = "Hetzner Cloud API token"
  type        = string
  sensitive   = true
  default     = "{{ .APIToken }}"
}

{{- if .SSHKeyID }}

data "hcloud_ssh_key" "default" {
  name = "{{ .SSHKeyID }}"
}
{{- end }}

{{- range $i, $spec := .Specs }}
{{- range $j := seq 0 $spec.Count }}

resource "hcloud_server" "{{ $.EnvName | replace "-" "_" }}_{{ $spec.Chassis | replace "." "_" }}_{{ $j }}" {
  name        = "{{ $.EnvName }}-{{ $spec.Chassis | splitList "." | last }}-{{ printf "%03d" (add $j 1) }}"
  server_type = "{{ $spec.OfferType }}"
  image       = "{{ $.Image }}"
  location    = "{{ $.Zone }}"
{{- if $.SSHKeyID }}
  ssh_keys    = [data.hcloud_ssh_key.default.id]
{{- end }}

  labels = {
    env        = "{{ $.EnvName }}"
    chassis    = "{{ $spec.Chassis }}"
    managed-by = "plasmactl"
  }
}
{{- end }}
{{- end }}

output "servers" {
  description = "Provisioned servers"
  value = {
{{- range $i, $spec := .Specs }}
{{- range $j := seq 0 $spec.Count }}
    "{{ $.EnvName }}-{{ $spec.Chassis | splitList "." | last }}-{{ printf "%03d" (add $j 1) }}" = {
      hostname    = hcloud_server.{{ $.EnvName | replace "-" "_" }}_{{ $spec.Chassis | replace "." "_" }}_{{ $j }}.name
      public_ip   = hcloud_server.{{ $.EnvName | replace "-" "_" }}_{{ $spec.Chassis | replace "." "_" }}_{{ $j }}.ipv4_address
      private_ip  = ""
      private_mac = ""
      server_id   = tostring(hcloud_server.{{ $.EnvName | replace "-" "_" }}_{{ $spec.Chassis | replace "." "_" }}_{{ $j }}.id)
      zone        = hcloud_server.{{ $.EnvName | replace "-" "_" }}_{{ $spec.Chassis | replace "." "_" }}_{{ $j }}.location
      region      = ""
      offer_name  = "{{ $spec.OfferType }}"
      chassis     = "{{ $spec.Chassis }}"
      provider    = "hetzner"
    }
{{- end }}
{{- end }}
  }
}
`
