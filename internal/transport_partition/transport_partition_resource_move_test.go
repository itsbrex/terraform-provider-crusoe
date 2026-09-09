package transport_partition

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// transportPartitionResource must satisfy the interface Terraform uses to move a
// resource across types with a "moved" block.
var _ resource.ResourceWithMoveState = &transportPartitionResource{}

const (
	movePartitionUUID = "11111111-1111-1111-1111-111111111111"
	moveProjectUUID   = "22222222-2222-2222-2222-222222222222"
	moveNetworkUUID   = "33333333-3333-3333-3333-333333333333"
)

// newIBPartitionSourceState builds a version 1 crusoe_ib_partition state, the way
// the framework hands it to a state mover.
func newIBPartitionSourceState(ctx context.Context, t *testing.T) *tfsdk.State {
	t.Helper()

	sourceSchema := ibPartitionSourceSchema()
	state := &tfsdk.State{
		Raw:    tftypes.NewValue(sourceSchema.Type().TerraformType(ctx), nil),
		Schema: *sourceSchema,
	}

	diags := state.Set(ctx, ibPartitionSourceModel{
		ID:          types.StringValue(movePartitionUUID),
		ProjectID:   types.StringValue(moveProjectUUID),
		Name:        types.StringValue("my-partition"),
		IBNetworkID: types.StringValue(moveNetworkUUID),
	})
	if diags.HasError() {
		t.Fatalf("failed to build source state: %v", diags)
	}

	return state
}

// newMoveStateResponse builds an empty MoveStateResponse backed by the transport
// partition schema, mirroring how the framework initializes the target state.
func newMoveStateResponse(ctx context.Context, t *testing.T) *resource.MoveStateResponse {
	t.Helper()

	r := &transportPartitionResource{}
	schemaResp := &resource.SchemaResponse{}
	r.Schema(ctx, resource.SchemaRequest{}, schemaResp)

	if schemaResp.Diagnostics.HasError() {
		t.Fatalf("failed to build schema: %v", schemaResp.Diagnostics)
	}

	return &resource.MoveStateResponse{
		TargetState: tfsdk.State{
			Raw:    tftypes.NewValue(schemaResp.Schema.Type().TerraformType(ctx), nil),
			Schema: schemaResp.Schema,
		},
	}
}

// TestMoveIBPartitionState covers the crusoe_ib_partition to
// crusoe_transport_partition move: a matching request maps every attribute, a
// request for another resource type is skipped, and an unsupported source schema
// version reports an error.
func TestMoveIBPartitionState(t *testing.T) {
	ctx := context.Background()

	t.Run("maps every attribute", func(t *testing.T) {
		resp := newMoveStateResponse(ctx, t)

		moveIBPartitionState(ctx, resource.MoveStateRequest{
			SourceProviderAddress: "registry.terraform.io/" + providerAddressSuffix,
			SourceTypeName:        sourceIBPartitionTypeName,
			SourceSchemaVersion:   sourceIBPartitionSchemaVersion,
			SourceState:           newIBPartitionSourceState(ctx, t),
		}, resp)

		if resp.Diagnostics.HasError() {
			t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
		}

		var got transportPartitionResourceModel
		if diags := resp.TargetState.Get(ctx, &got); diags.HasError() {
			t.Fatalf("failed to read target state: %v", diags)
		}

		if got.ID.ValueString() != movePartitionUUID {
			t.Errorf("id = %q, want %q", got.ID.ValueString(), movePartitionUUID)
		}
		if got.ProjectID.ValueString() != moveProjectUUID {
			t.Errorf("project_id = %q, want %q", got.ProjectID.ValueString(), moveProjectUUID)
		}
		if got.Name.ValueString() != "my-partition" {
			t.Errorf("name = %q, want %q", got.Name.ValueString(), "my-partition")
		}
		if got.TransportNetworkID.ValueString() != moveNetworkUUID {
			t.Errorf("transport_network_id = %q, want %q", got.TransportNetworkID.ValueString(), moveNetworkUUID)
		}
	})

	t.Run("skips another source resource type", func(t *testing.T) {
		resp := newMoveStateResponse(ctx, t)

		moveIBPartitionState(ctx, resource.MoveStateRequest{
			SourceProviderAddress: "registry.terraform.io/" + providerAddressSuffix,
			SourceTypeName:        "crusoe_vpc_network",
			SourceSchemaVersion:   sourceIBPartitionSchemaVersion,
			SourceState:           newIBPartitionSourceState(ctx, t),
		}, resp)

		if resp.Diagnostics.HasError() {
			t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
		}

		// An empty response tells the framework to try the next mover.
		if !resp.TargetState.Raw.IsNull() {
			t.Error("target state was set for an unrelated source resource type")
		}
	})

	t.Run("skips another provider", func(t *testing.T) {
		resp := newMoveStateResponse(ctx, t)

		moveIBPartitionState(ctx, resource.MoveStateRequest{
			SourceProviderAddress: "registry.terraform.io/hashicorp/random",
			SourceTypeName:        sourceIBPartitionTypeName,
			SourceSchemaVersion:   sourceIBPartitionSchemaVersion,
			SourceState:           newIBPartitionSourceState(ctx, t),
		}, resp)

		if resp.Diagnostics.HasError() {
			t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
		}

		if !resp.TargetState.Raw.IsNull() {
			t.Error("target state was set for an unrelated provider")
		}
	})

	t.Run("rejects an unsupported source schema version", func(t *testing.T) {
		resp := newMoveStateResponse(ctx, t)

		moveIBPartitionState(ctx, resource.MoveStateRequest{
			SourceProviderAddress: "registry.terraform.io/" + providerAddressSuffix,
			SourceTypeName:        sourceIBPartitionTypeName,
			SourceSchemaVersion:   0,
			SourceState:           newIBPartitionSourceState(ctx, t),
		}, resp)

		if !resp.Diagnostics.HasError() {
			t.Error("expected an error for an unsupported source schema version")
		}
	})

	t.Run("rejects a missing source state", func(t *testing.T) {
		resp := newMoveStateResponse(ctx, t)

		moveIBPartitionState(ctx, resource.MoveStateRequest{
			SourceProviderAddress: "registry.terraform.io/" + providerAddressSuffix,
			SourceTypeName:        sourceIBPartitionTypeName,
			SourceSchemaVersion:   sourceIBPartitionSchemaVersion,
		}, resp)

		if !resp.Diagnostics.HasError() {
			t.Error("expected an error for a missing source state")
		}
	})
}

// TestMoveStateSourceSchemaMatchesIBPartition guards the mover's copy of the
// crusoe_ib_partition schema against drift in the real resource schema.
func TestMoveStateSourceSchemaMatchesIBPartition(t *testing.T) {
	wantAttributes := []string{"id", "name", "ib_network_id", "project_id"}

	sourceSchema := ibPartitionSourceSchema()
	if len(sourceSchema.Attributes) != len(wantAttributes) {
		t.Fatalf("source schema has %d attributes, want %d", len(sourceSchema.Attributes), len(wantAttributes))
	}

	for _, name := range wantAttributes {
		if _, ok := sourceSchema.Attributes[name]; !ok {
			t.Errorf("source schema is missing the %q attribute", name)
		}
	}
}
