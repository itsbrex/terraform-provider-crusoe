package disk

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	swagger "github.com/crusoecloud/client-go/swagger/v1alpha5"
	"github.com/crusoecloud/terraform-provider-crusoe/internal/common"
	validators "github.com/crusoecloud/terraform-provider-crusoe/internal/validators"
)

const (
	persistentSSD      = "persistent-ssd"
	sharedVolume       = "shared-volume"
	gibInTib           = 1024
	alternateBlockSize = 512
	defaultBlockSize   = 4096
)

type diskResource struct {
	client *swagger.APIClient
}

type diskResourceModel struct {
	ID           types.String `tfsdk:"id"`
	ProjectID    types.String `tfsdk:"project_id"`
	Location     types.String `tfsdk:"location"`
	Name         types.String `tfsdk:"name"`
	Type         types.String `tfsdk:"type"`
	Size         types.String `tfsdk:"size"`
	SerialNumber types.String `tfsdk:"serial_number"`
	BlockSize    types.Int64  `tfsdk:"block_size"`
}

func NewDiskResource() resource.Resource {
	return &diskResource{}
}

//nolint:gocritic // Implements Terraform defined interface
func (r *diskResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	client, ok := req.ProviderData.(*swagger.APIClient)
	if !ok {
		resp.Diagnostics.AddError("Failed to initialize provider", common.ErrorMsgProviderInitFailed)

		return
	}

	r.client = client
}

//nolint:gocritic // Implements Terraform defined interface
func (r *diskResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_storage_disk"
}

//nolint:gocritic,gomnd // Implements Terraform defined interface
func (r *diskResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Version: 1,
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}, // maintain across updates
			},
			"project_id": schema.StringAttribute{
				Optional: true,
				Computed: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
					stringplanmodifier.RequiresReplace(),
				},
			},
			"location": schema.StringAttribute{
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}, // cannot be updated in place
			},
			"name": schema.StringAttribute{
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}, // cannot be updated in place
			},
			"type": schema.StringAttribute{
				Optional:   true,
				Computed:   true,
				Validators: []validator.String{stringvalidator.OneOf(persistentSSD, sharedVolume)},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),    // cannot be updated in place
					stringplanmodifier.UseStateForUnknown(), // maintain across updates if not explicitly changed
				},
			},
			"size": schema.StringAttribute{
				Required:   true,
				Validators: []validator.String{validators.StorageSizeValidator{}},
			},
			"serial_number": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}, // maintain across updates
			},
			"block_size": schema.Int64Attribute{
				Optional:   true,
				Computed:   true,
				Validators: []validator.Int64{int64validator.OneOf(alternateBlockSize, defaultBlockSize)}, // we support either 512 or 4096 bits
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.RequiresReplaceIfConfigured(), // cannot be updated in place
					int64planmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *diskResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resourceIdentifiers := strings.Split(req.ID, ",")

	// We allow "disk_id" (project_id is implicitly defined via env variable) or "disk_id,project_id" (explicit project_id)
	diskID := resourceIdentifiers[0]
	var projectID string

	switch len(resourceIdentifiers) {
	case 1:
		projectID = ""
	case 2:
		projectID = resourceIdentifiers[1]
	default:
		resp.Diagnostics.AddError("Invalid resource identifier", fmt.Sprintf("Expected format disk_id,project_id, got %q", req.ID))

		return
	}

	projectID, err := common.GetProjectIDOrFallback(ctx, r.client, &resp.Diagnostics, projectID)
	if err != nil {
		resp.Diagnostics.AddError("Failed to fetch project ID",
			fmt.Sprintf("No project was specified and it was not possible to determine which project to use: %v", err))

		return
	}

	if _, parseErr := uuid.Parse(diskID); parseErr != nil {
		resp.Diagnostics.AddError("Invalid resource identifier", fmt.Sprintf("Failed to parse disk ID: %v", parseErr))

		return
	}

	if _, parseErr := uuid.Parse(projectID); parseErr != nil {
		resp.Diagnostics.AddError("Invalid resource identifier", fmt.Sprintf("Failed to parse project ID: %v", parseErr))

		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), diskID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("project_id"), projectID)...)
}

//nolint:gocritic // Implements Terraform defined interface
func (r *diskResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan diskResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	diskType := plan.Type.ValueString()
	if diskType == "" {
		diskType = persistentSSD
	}

	projectID := ""
	if plan.ProjectID.ValueString() == "" {
		project, err := common.GetFallbackProject(ctx, r.client, &resp.Diagnostics)
		if err != nil {
			resp.Diagnostics.AddError("Failed to create disk",
				fmt.Sprintf("No project was specified and it was not possible to determine which project to use: %v", err))

			return
		}
		projectID = project
	} else {
		projectID = plan.ProjectID.ValueString()
	}

	blockSize := plan.BlockSize.ValueInt64()
	if blockSize == 0 && diskType == persistentSSD {
		blockSize = defaultBlockSize
	}

	dataResp, httpResp, err := r.client.DisksApi.CreateDisk(ctx, swagger.DisksPostRequestV1Alpha5{
		Name:      plan.Name.ValueString(),
		Location:  plan.Location.ValueString(),
		Type_:     diskType,
		Size:      plan.Size.ValueString(),
		BlockSize: blockSize,
	}, projectID)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create disk",
			fmt.Sprintf("There was an error starting a create disk operation (%s): %s", projectID, common.UnpackAPIError(err)))

		return
	}
	defer httpResp.Body.Close()

	disk, _, err := common.AwaitOperationAndResolve[swagger.DiskV1Alpha5](ctx, dataResp.Operation, projectID, r.client.DiskOperationsApi.GetStorageDisksOperation)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create disk",
			fmt.Sprintf("There was an error creating a disk: %s", common.UnpackAPIError(err)))

		return
	}

	plan.ID = types.StringValue(disk.Id)
	plan.Type = types.StringValue(disk.Type_)
	plan.Location = types.StringValue(disk.Location)
	plan.SerialNumber = types.StringValue(disk.SerialNumber)
	plan.Size = types.StringValue(formatSize(plan.Size.ValueString(), disk.Size))
	plan.ProjectID = types.StringValue(projectID)
	plan.BlockSize = types.Int64Value(disk.BlockSize)

	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
}

//nolint:gocritic // Implements Terraform defined interface
func (r *diskResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state diskResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// We only have this parsing for transitioning from v1alpha4 to v1alpha5 because old tf state files will not
	// have project ID stored. So we will try to get a fallback project to pass to the API.
	var projectID string
	if state.ProjectID.ValueString() == "" {
		project, err := common.GetFallbackProject(ctx, r.client, &resp.Diagnostics)
		if err != nil {
			resp.Diagnostics.AddError("Failed to create disk",
				fmt.Sprintf("No project was specified and it was not possible to determine which project to use: %v", err))

			return
		}
		projectID = project
	} else {
		projectID = state.ProjectID.ValueString()
	}

	dataResp, httpResp, err := r.client.DisksApi.ListDisks(ctx, projectID, &swagger.DisksApiListDisksOpts{})
	if err != nil {
		resp.Diagnostics.AddError("Failed to get disks",
			fmt.Sprintf("Fetching Crusoe disks failed: %s\n\nIf the problem persists, contact support@crusoecloud.com", common.UnpackAPIError(err)))

		return
	}
	defer httpResp.Body.Close()

	var disk *swagger.DiskV1Alpha5
	for i := range dataResp.Items {
		if dataResp.Items[i].Id == state.ID.ValueString() {
			disk = &dataResp.Items[i]
		}
	}

	if disk == nil {
		// disk has most likely been deleted out of band, so we update Terraform state to match
		resp.State.RemoveResource(ctx)

		return
	}

	diskToTerraformResourceModel(disk, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

//nolint:gocritic // Implements Terraform defined interface
func (r *diskResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var state diskResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	var plan diskResourceModel
	diags = req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	dataResp, httpResp, err := r.client.DisksApi.ResizeDisk(ctx,
		swagger.DisksPatchRequest{Size: plan.Size.ValueString()},
		plan.ProjectID.ValueString(),
		plan.ID.ValueString(),
	)
	if err != nil {
		resp.Diagnostics.AddError("Failed to resize disk",
			fmt.Sprintf("There was an error starting a resize operation: %s.\n\n"+
				"Make sure the disk still exists, you are enlarging the disk,"+
				" and if the disk is attached to a VM, the VM is powered off.", common.UnpackAPIError(err)))

		return
	}
	defer httpResp.Body.Close()

	_, _, err = common.AwaitOperationAndResolve[swagger.DiskV1Alpha5](ctx, dataResp.Operation, plan.ProjectID.ValueString(), r.client.DiskOperationsApi.GetStorageDisksOperation)
	if err != nil {
		resp.Diagnostics.AddError("Failed to resize disk",
			fmt.Sprintf("There was an error resizing a disk: %s.\n\n"+
				"Make sure the disk still exists, you are enlarging the disk,"+
				" and if the disk is attached to a VM, the VM is powered off.", common.UnpackAPIError(err)))

		return
	}

	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
}

//nolint:gocritic // Implements Terraform defined interface
func (r *diskResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state diskResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	dataResp, httpResp, err := r.client.DisksApi.DeleteDisk(ctx, state.ProjectID.ValueString(), state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to delete disk",
			fmt.Sprintf("There was an error starting a delete disk operation: %s", common.UnpackAPIError(err)))

		return
	}
	defer httpResp.Body.Close()

	_, err = common.AwaitOperation(ctx, dataResp.Operation, state.ProjectID.ValueString(), r.client.DiskOperationsApi.GetStorageDisksOperation)
	if err != nil {
		resp.Diagnostics.AddError("Failed to delete disk",
			fmt.Sprintf("There was an error deleting a disk: %s", common.UnpackAPIError(err)))

		return
	}
}

// formatSize takes a format size to use as a pattern and converts the sizeStr to match it.
func formatSize(format, sizeStr string) string {
	lowerFormatSize := strings.ToLower(format)
	lowerSize := strings.ToLower(sizeStr)
	if strings.HasSuffix(lowerFormatSize, "tib") && strings.HasSuffix(lowerSize, "gib") {
		if size, err := strconv.Atoi(sizeStr[:len(sizeStr)-3]); err == nil &&
			size >= gibInTib && size%gibInTib == 0 {

			return strconv.Itoa(size/gibInTib) + "TiB"
		}

		return sizeStr
	}

	if strings.HasSuffix(lowerFormatSize, "gib") && strings.HasSuffix(lowerSize, "tib") {
		if size, err := strconv.Atoi(sizeStr[:len(sizeStr)-3]); err == nil {
			return strconv.Itoa(size*gibInTib) + "GiB"
		}

		return sizeStr
	}

	return sizeStr
}
