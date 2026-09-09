//nolint:gocritic // Implements Terraform defined interface
package transport_partition

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

const (
	// sourceIBPartitionTypeName is the resource type a "moved" block can move from.
	sourceIBPartitionTypeName = "crusoe_ib_partition"

	// sourceIBPartitionSchemaVersion is the crusoe_ib_partition schema version this
	// mover reads. Version 0 predates project_id, so it does not carry enough data
	// to build a transport partition state.
	sourceIBPartitionSchemaVersion = 1

	// providerAddressSuffix matches the provider source address without the
	// hostname, so a state written through a registry mirror still moves.
	providerAddressSuffix = "crusoecloud/crusoe"
)

// ibPartitionSourceModel mirrors the version 1 crusoe_ib_partition state. It lives
// here instead of in the ib_partition package to keep the move self-contained: the
// mover must keep reading the old shape even after that resource is removed.
type ibPartitionSourceModel struct {
	ID          types.String `tfsdk:"id"`
	ProjectID   types.String `tfsdk:"project_id"`
	Name        types.String `tfsdk:"name"`
	IBNetworkID types.String `tfsdk:"ib_network_id"`
}

// ibPartitionSourceSchema describes the version 1 crusoe_ib_partition schema. The
// framework uses it to decode the source state, so only the attribute names and
// types matter here. Validators and plan modifiers do not apply to a move.
func ibPartitionSourceSchema() *schema.Schema {
	return &schema.Schema{
		Version: sourceIBPartitionSchemaVersion,
		Attributes: map[string]schema.Attribute{
			"id":            schema.StringAttribute{Computed: true},
			"name":          schema.StringAttribute{Required: true},
			"ib_network_id": schema.StringAttribute{Required: true},
			"project_id":    schema.StringAttribute{Optional: true, Computed: true},
		},
	}
}

// MoveState lets users migrate a crusoe_ib_partition to a crusoe_transport_partition
// with a "moved" block instead of removing and importing the resource. Both resource
// types back the same API object, so the move only renames ib_network_id to
// transport_network_id and keeps the partition in place.
//
// This needs Terraform 1.8 or later. Terraform does not call Configure before a
// move, so the client is nil here and the mover must not call the API.
func (r *transportPartitionResource) MoveState(context.Context) []resource.StateMover {
	return []resource.StateMover{
		{
			SourceSchema: ibPartitionSourceSchema(),
			StateMover:   moveIBPartitionState,
		},
	}
}

func moveIBPartitionState(ctx context.Context, req resource.MoveStateRequest, resp *resource.MoveStateResponse) {
	// A target resource can have many movers, so a request for something else is
	// not an error. Leave the response empty and the framework tries the next one.
	if req.SourceTypeName != sourceIBPartitionTypeName ||
		!strings.HasSuffix(req.SourceProviderAddress, providerAddressSuffix) {

		return
	}

	if req.SourceSchemaVersion != sourceIBPartitionSchemaVersion {
		resp.Diagnostics.AddError("Unable to move IB partition to transport partition",
			fmt.Sprintf("The %s state is at schema version %d, but the move needs version %d."+
				" Run `terraform apply -refresh-only` with this provider version first to upgrade the state,"+
				" then apply the moved block.",
				sourceIBPartitionTypeName, req.SourceSchemaVersion, sourceIBPartitionSchemaVersion))

		return
	}

	// SourceState is nil when the source state does not fit the declared schema.
	if req.SourceState == nil {
		resp.Diagnostics.AddError("Unable to move IB partition to transport partition",
			fmt.Sprintf("The %s state did not match the expected version %d schema."+
				" Contact support@crusoecloud.com if the problem persists.",
				sourceIBPartitionTypeName, sourceIBPartitionSchemaVersion))

		return
	}

	var source ibPartitionSourceModel
	resp.Diagnostics.Append(req.SourceState.Get(ctx, &source)...)

	if resp.Diagnostics.HasError() {
		return
	}

	if source.ID.IsNull() || source.ID.ValueString() == "" {
		resp.Diagnostics.AddError("Unable to move IB partition to transport partition",
			"No ID was associated with the IB partition.")

		return
	}

	resp.Diagnostics.Append(resp.TargetState.Set(ctx, transportPartitionResourceModel{
		ID:                 source.ID,
		ProjectID:          source.ProjectID,
		Name:               source.Name,
		TransportNetworkID: source.IBNetworkID,
	})...)
}
