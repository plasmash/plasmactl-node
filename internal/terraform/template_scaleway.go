package terraform

func init() {
	RegisterTemplate("scaleway", scalewayDediboxTemplate)
}

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
  default     = "{{ .ProjectID }}"
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
      private_ip  = ""
      private_mac = scaleway_dedibox_server.{{ $.EnvName | replace "-" "_" }}_{{ $spec.Chassis | replace "." "_" }}_{{ $j }}.interfaces[0].mac_address
      server_id   = scaleway_dedibox_server.{{ $.EnvName | replace "-" "_" }}_{{ $spec.Chassis | replace "." "_" }}_{{ $j }}.id
      zone        = scaleway_dedibox_server.{{ $.EnvName | replace "-" "_" }}_{{ $spec.Chassis | replace "." "_" }}_{{ $j }}.zone
      region      = ""
      offer_name  = "{{ $spec.OfferType }}"
      chassis     = "{{ $spec.Chassis }}"
      provider    = "scaleway"
    }
{{- end }}
{{- end }}
  }
}
`
