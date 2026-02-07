package terraform

func init() {
	RegisterTemplate("aws", awsEC2Template)
}

const awsEC2Template = `
terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = ">= 5.0.0"
    }
  }
}

provider "aws" {
  region = "{{ .Region }}"
}

{{- if .SSHKeyID }}

data "aws_key_pair" "default" {
  key_name = "{{ .SSHKeyID }}"
}
{{- end }}

{{- range $i, $spec := .Specs }}
{{- range $j := seq 0 $spec.Count }}

resource "aws_instance" "{{ $.EnvName | replace "-" "_" }}_{{ $spec.Chassis | replace "." "_" }}_{{ $j }}" {
  ami               = "{{ $.Image }}"
  instance_type     = "{{ $spec.OfferType }}"
  availability_zone = "{{ $.Zone }}"
{{- if $.SSHKeyID }}
  key_name          = data.aws_key_pair.default.key_name
{{- end }}

  tags = {
    Name       = "{{ $.EnvName }}-{{ $spec.Chassis | splitList "." | last }}-{{ printf "%03d" (add $j 1) }}"
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
      hostname    = aws_instance.{{ $.EnvName | replace "-" "_" }}_{{ $spec.Chassis | replace "." "_" }}_{{ $j }}.tags["Name"]
      public_ip   = aws_instance.{{ $.EnvName | replace "-" "_" }}_{{ $spec.Chassis | replace "." "_" }}_{{ $j }}.public_ip
      private_ip  = aws_instance.{{ $.EnvName | replace "-" "_" }}_{{ $spec.Chassis | replace "." "_" }}_{{ $j }}.private_ip
      private_mac = ""
      server_id   = aws_instance.{{ $.EnvName | replace "-" "_" }}_{{ $spec.Chassis | replace "." "_" }}_{{ $j }}.id
      zone        = aws_instance.{{ $.EnvName | replace "-" "_" }}_{{ $spec.Chassis | replace "." "_" }}_{{ $j }}.availability_zone
      region      = "{{ $.Region }}"
      offer_name  = "{{ $spec.OfferType }}"
      chassis     = "{{ $spec.Chassis }}"
      provider    = "aws"
    }
{{- end }}
{{- end }}
  }
}
`
