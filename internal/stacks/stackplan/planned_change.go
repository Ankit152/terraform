package stackplan

import (
	"fmt"

	version "github.com/hashicorp/go-version"
	"github.com/zclconf/go-cty/cty"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/hashicorp/terraform/internal/plans"
	"github.com/hashicorp/terraform/internal/plans/planfile"
	"github.com/hashicorp/terraform/internal/rpcapi/terraform1"
	"github.com/hashicorp/terraform/internal/stacks/stackaddrs"
	"github.com/hashicorp/terraform/internal/stacks/tfstackdata1"
)

// PlannedChange represents a single isolated planned changed, emitted as
// part of a stream of planned changes during the PlanStackChanges RPC API
// operation.
//
// Each PlannedChange becomes a single event in the RPC API, which itself
// has zero or more opaque raw plan messages that the caller must collect and
// provide verbatim during planning and zero or one "description" messages
// that are to give the caller realtime updates about the planning process.
//
// The aggregated sequence of "raw" messages can be provided later to
// [LoadFromProto] to obtain a [Plan] object containing the information
// Terraform Core would need to apply the plan.
type PlannedChange interface {
	// PlannedChangeProto returns the protocol buffers representation of
	// the change, ready to be sent verbatim to an RPC API client.
	PlannedChangeProto() (*terraform1.PlannedChange, error)
}

// PlannedChangeComponentInstance announces the existence of a component
// instance and describes (using a plan action) whether it is being added
// or removed.
type PlannedChangeComponentInstance struct {
	Addr stackaddrs.AbsComponentInstance

	// Action describes any difference in the existence of this component
	// instance compared to the prior state.
	//
	// Currently it can only be "Create", "Delete", or "NoOp". This action
	// relates to the existence of the component instance itself and does
	// not consider the resource instances inside, whose change actions
	// are tracked in their own [PlannedChange] objects.
	Action plans.Action
}

var _ PlannedChange = (*PlannedChangeComponentInstance)(nil)

// PlannedChangeProto implements PlannedChange.
func (pc *PlannedChangeComponentInstance) PlannedChangeProto() (*terraform1.PlannedChange, error) {
	var raw anypb.Any
	err := anypb.MarshalFrom(&raw, &tfstackdata1.PlanComponentInstance{
		ComponentInstanceAddr: pc.Addr.String(),
		// We don't track the action as part of the raw data because we
		// don't actually need it to apply the change; it's only included
		// for external consumption, such as rendering changes in the UI.
	}, proto.MarshalOptions{})
	if err != nil {
		return nil, err
	}

	protoChangeTypes, err := terraform1.ChangeTypesForPlanAction(pc.Action)
	if err != nil {
		return nil, err
	}

	return &terraform1.PlannedChange{
		Raw: []*anypb.Any{&raw},
		Description: &terraform1.PlannedChange_ComponentInstancePlanned{
			ComponentInstancePlanned: &terraform1.PlannedChange_ComponentInstance{
				ComponentAddr:         stackaddrs.ConfigComponentForAbsInstance(pc.Addr).String(),
				ComponentInstanceAddr: pc.Addr.String(),
				Actions:               protoChangeTypes,
			},
		},
	}, nil
}

// PlannedChangeResourceInstancePlanned announces an action that Terraform
// is proposing to take if this plan is applied.
type PlannedChangeResourceInstancePlanned struct {
	ComponentInstanceAddr stackaddrs.AbsComponentInstance
	ChangeSrc             *plans.ResourceInstanceChangeSrc
}

var _ PlannedChange = (*PlannedChangeResourceInstancePlanned)(nil)

// PlannedChangeProto implements PlannedChange.
func (pc *PlannedChangeResourceInstancePlanned) PlannedChangeProto() (*terraform1.PlannedChange, error) {
	if pc.ChangeSrc == nil {
		return nil, fmt.Errorf("nil ChangeSrc")
	}

	changeProto, err := planfile.ResourceChangeToProto(pc.ChangeSrc)
	if err != nil {
		return nil, fmt.Errorf("converting resource instance change to proto: %w", err)
	}
	var raw anypb.Any
	err = anypb.MarshalFrom(&raw, &tfstackdata1.PlanResourceInstanceChangePlanned{
		ComponentInstanceAddr: pc.ComponentInstanceAddr.String(),
		Change:                changeProto,
	}, proto.MarshalOptions{})
	if err != nil {
		return nil, err
	}

	protoChangeTypes, err := terraform1.ChangeTypesForPlanAction(pc.ChangeSrc.Action)
	if err != nil {
		return nil, err
	}

	return &terraform1.PlannedChange{
		Raw: []*anypb.Any{&raw},
		Description: &terraform1.PlannedChange_ResourceInstancePlanned{
			ResourceInstancePlanned: &terraform1.PlannedChange_ResourceInstance{
				Addr: &terraform1.ResourceInstanceInStackAddr{
					ComponentInstanceAddr: pc.ComponentInstanceAddr.String(),
					ResourceInstanceAddr:  pc.ChangeSrc.Addr.String(),
				},
				Actions: protoChangeTypes,
				Values: &terraform1.DynamicValueChange{
					Old: terraform1.NewDynamicValue(pc.ChangeSrc.Before, pc.ChangeSrc.BeforeValMarks),
					New: terraform1.NewDynamicValue(pc.ChangeSrc.After, pc.ChangeSrc.AfterValMarks),
				},
				// TODO: Moved, Imported
			},
		},
	}, nil
}

// PlannedChangeResourceInstanceOutside announces that Terraform has detected
// some action taken outside of Terraform since the last apply.
type PlannedChangeResourceInstanceOutside struct {
	ComponentInstanceAddr stackaddrs.AbsComponentInstance
	ChangeSrc             *plans.ResourceInstanceChangeSrc
}

var _ PlannedChange = (*PlannedChangeResourceInstanceOutside)(nil)

// PlannedChangeProto implements PlannedChange.
func (pc *PlannedChangeResourceInstanceOutside) PlannedChangeProto() (*terraform1.PlannedChange, error) {
	if pc.ChangeSrc == nil {
		return nil, fmt.Errorf("nil ChangeSrc")
	}

	changeProto, err := planfile.ResourceChangeToProto(pc.ChangeSrc)
	if err != nil {
		return nil, fmt.Errorf("converting resource instance change to proto: %w", err)
	}
	var raw anypb.Any
	err = anypb.MarshalFrom(&raw, &tfstackdata1.PlanResourceInstanceChangeOutside{
		ComponentInstanceAddr: pc.ComponentInstanceAddr.String(),
		Change:                changeProto,
	}, proto.MarshalOptions{})
	if err != nil {
		return nil, err
	}

	protoChangeTypes, err := terraform1.ChangeTypesForPlanAction(pc.ChangeSrc.Action)
	if err != nil {
		return nil, err
	}

	return &terraform1.PlannedChange{
		Raw: []*anypb.Any{&raw},
		Description: &terraform1.PlannedChange_ResourceInstanceDrifted{
			ResourceInstanceDrifted: &terraform1.PlannedChange_ResourceInstance{
				Addr: &terraform1.ResourceInstanceInStackAddr{
					ComponentInstanceAddr: pc.ComponentInstanceAddr.String(),
					ResourceInstanceAddr:  pc.ChangeSrc.Addr.String(),
				},
				Actions: protoChangeTypes,
				Values: &terraform1.DynamicValueChange{
					Old: terraform1.NewDynamicValue(pc.ChangeSrc.Before, pc.ChangeSrc.BeforeValMarks),
					New: terraform1.NewDynamicValue(pc.ChangeSrc.After, pc.ChangeSrc.AfterValMarks),
				},
				// TODO: Moved, Imported
			},
		},
	}, nil
}

// PlannedChangeOutputValue announces the change action for one output value
// declared in the top-level stack configuration.
//
// This change type only includes an external description, and does not
// contribute anything to the raw plan sequence.
type PlannedChangeOutputValue struct {
	Addr   stackaddrs.OutputValue // Covers only root stack output values
	Action plans.Action

	OldValue, NewValue           plans.DynamicValue
	OldValueMarks, NewValueMarks []cty.PathValueMarks
	// TODO: We'll need to encode the old and new _types_ here too, because
	// they aren't available from a schema as is the case for the similar
	// value fields in PlannedChangeResourceInstancePlanned.
}

var _ PlannedChange = (*PlannedChangeOutputValue)(nil)

// PlannedChangeProto implements PlannedChange.
func (pc *PlannedChangeOutputValue) PlannedChangeProto() (*terraform1.PlannedChange, error) {
	protoChangeTypes, err := terraform1.ChangeTypesForPlanAction(pc.Action)
	if err != nil {
		return nil, err
	}

	return &terraform1.PlannedChange{
		// No "raw" representation for output values; we emit them only for
		// external consumption, since Terraform Core will just recalculate
		// them during apply anyway.
		Description: &terraform1.PlannedChange_OutputValuePlanned{
			OutputValuePlanned: &terraform1.PlannedChange_OutputValue{
				Name:    pc.Addr.Name,
				Actions: protoChangeTypes,

				Values: &terraform1.DynamicValueChange{
					Old: terraform1.NewDynamicValue(pc.OldValue, pc.OldValueMarks),
					New: terraform1.NewDynamicValue(pc.NewValue, pc.NewValueMarks),
				},
			},
		},
	}, nil
}

// PlannedChangeHeader is a special change type we typically emit before any
// others to capture overall metadata about a plan. [LoadFromProto] fails if
// asked to decode a plan sequence that doesn't include at least one raw
// message generated from this change type.
//
// PlannedChangeHeader has only a raw message and does not contribute to
// the external-facing plan description.
type PlannedChangeHeader struct {
	TerraformVersion *version.Version
}

var _ PlannedChange = (*PlannedChangeHeader)(nil)

// PlannedChangeProto implements PlannedChange.
func (pc *PlannedChangeHeader) PlannedChangeProto() (*terraform1.PlannedChange, error) {
	var raw anypb.Any
	err := anypb.MarshalFrom(&raw, &tfstackdata1.PlanHeader{
		TerraformVersion: pc.TerraformVersion.String(),
	}, proto.MarshalOptions{})
	if err != nil {
		return nil, err
	}

	return &terraform1.PlannedChange{
		Raw: []*anypb.Any{&raw},
	}, nil
}

// PlannedChangeApplyable is a special change type we typically append at the
// end of the raw plan stream to represent that the planning process ran to
// completion without encountering any errors, and therefore the plan could
// potentially be applied.
type PlannedChangeApplyable struct {
	Applyable bool
}

var _ PlannedChange = (*PlannedChangeApplyable)(nil)

// PlannedChangeProto implements PlannedChange.
func (pc *PlannedChangeApplyable) PlannedChangeProto() (*terraform1.PlannedChange, error) {
	var raw anypb.Any
	err := anypb.MarshalFrom(&raw, &tfstackdata1.PlanApplyable{
		Applyable: pc.Applyable,
	}, proto.MarshalOptions{})
	if err != nil {
		return nil, err
	}

	return &terraform1.PlannedChange{
		Raw: []*anypb.Any{&raw},
		Description: &terraform1.PlannedChange_PlanApplyable{
			PlanApplyable: pc.Applyable,
		},
	}, nil
}
