terraform {
  required_providers {
    crusoe = {
      source = "registry.terraform.io/crusoecloud/crusoe"
    }
  }
}

locals {
  my_ssh_key = file("~/.ssh/id_ed25519.pub")
}

# list instance templates
data "crusoe_instance_templates" "my_templates" {}
output "crusoe_templates" {
  value = data.crusoe_instance_templates.my_templates
}

// new template
resource "crusoe_instance_template" "my_template" {
  name = "my-new-template"
  type = "a40.1x"
  location = "us-northcentral1-a"
  // this can be obtained via the `crusoe networking vpc-subnets list` CLI command
  subnet = "bd247b17-fd13-44ba-8aa8-703852b6f326"
  // optional parameter to configure placement policy, only "spread" is currently supported
  // defaults to "unspecified" if not provided
  placement_policy = "spread"

  # specify the base image
  image = "ubuntu20.04:latest"

  disks = [
      // disk to create for each VM
      {
        size = "10GiB"
        type = "persistent-ssd"
      }
    ]

  ssh_key = local.my_ssh_key

}

// create vm from template, with a name of my-new-vm-N
resource "crusoe_compute_instance_by_template" "my_vm" {
  name_prefix = "my-new-vm"
  instance_template = crusoe_instance_template.my_template.id
  count = 3
}