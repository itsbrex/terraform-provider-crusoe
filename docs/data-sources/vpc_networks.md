---
# generated by https://github.com/hashicorp/terraform-plugin-docs
page_title: "crusoe_vpc_networks Data Source - terraform-provider-crusoe"
subcategory: ""
description: |-
  
---

# crusoe_vpc_networks (Data Source)





<!-- schema generated by tfplugindocs -->
## Schema

### Optional

- `project_id` (String)

### Read-Only

- `vpc_networks` (Attributes List) (see [below for nested schema](#nestedatt--vpc_networks))

<a id="nestedatt--vpc_networks"></a>
### Nested Schema for `vpc_networks`

Required:

- `cidr` (String)
- `gateway` (String)
- `name` (String)

Optional:

- `subnets` (List of String)

Read-Only:

- `id` (String)
