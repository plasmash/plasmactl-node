package join

import (
	"errors"
	"testing"

	"github.com/plasmash/plasmactl-platform/pkg/provisioner"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakePrompter records calls and returns canned responses.
type fakePrompter struct {
	yesNoCalls    int
	typedYesCalls int
	yesNoAnswer   bool
	typedAnswer   bool
	yesNoErr      error
	typedErr      error
}

func (p *fakePrompter) askSimpleYN(_ string) (bool, error) {
	p.yesNoCalls++
	return p.yesNoAnswer, p.yesNoErr
}

func (p *fakePrompter) askTypedYes(_ string) (bool, error) {
	p.typedYesCalls++
	return p.typedAnswer, p.typedErr
}

func TestClassify_NoOp_AutoApprove_Proceeds(t *testing.T) {
	j := &Join{AutoApprove: true}
	s := &provisioner.PlanSummary{}
	p := &fakePrompter{}
	proceed, err := classifyAndConfirm(j, s, p)
	require.NoError(t, err)
	assert.True(t, proceed)
	assert.Equal(t, 0, p.yesNoCalls)
	assert.Equal(t, 0, p.typedYesCalls)
}

func TestClassify_NoOp_Interactive_PromptsSimpleYN(t *testing.T) {
	j := &Join{}
	s := &provisioner.PlanSummary{}
	p := &fakePrompter{yesNoAnswer: true}
	proceed, err := classifyAndConfirm(j, s, p)
	require.NoError(t, err)
	assert.True(t, proceed)
	assert.Equal(t, 1, p.yesNoCalls)
	assert.Equal(t, 0, p.typedYesCalls)
}

func TestClassify_NoOp_Interactive_DeclinedDoesNotProceed(t *testing.T) {
	j := &Join{}
	s := &provisioner.PlanSummary{}
	p := &fakePrompter{yesNoAnswer: false}
	proceed, err := classifyAndConfirm(j, s, p)
	require.NoError(t, err)
	assert.False(t, proceed)
}

func TestClassify_DeletePresent_HardRefuses(t *testing.T) {
	j := &Join{}
	s := &provisioner.PlanSummary{
		Destroys: 1,
		Deletes: []provisioner.ResourceImpact{
			{Address: "ovh_dedicated_server.example_plat_control_0", Type: "ovh_dedicated_server", Name: "example_plat_control_0"},
		},
	}
	p := &fakePrompter{}
	proceed, err := classifyAndConfirm(j, s, p)
	require.Error(t, err)
	assert.False(t, proceed)
	assert.Contains(t, err.Error(), "node:join refused")
	assert.Contains(t, err.Error(), "delete")
	assert.Contains(t, err.Error(), "ovh_dedicated_server.example_plat_control_0")
	assert.Contains(t, err.Error(), "node:destroy")
	// No prompts are asked when we hard-refuse.
	assert.Equal(t, 0, p.yesNoCalls)
	assert.Equal(t, 0, p.typedYesCalls)
}

func TestClassify_ReplacePresent_HardRefuses(t *testing.T) {
	j := &Join{}
	s := &provisioner.PlanSummary{
		Replaces: 1,
		Replacements: []provisioner.ResourceImpact{
			{Address: "ovh_dedicated_server.example_plat_control_0", Type: "ovh_dedicated_server", Name: "example_plat_control_0"},
		},
	}
	p := &fakePrompter{}
	proceed, err := classifyAndConfirm(j, s, p)
	require.Error(t, err)
	assert.False(t, proceed)
	assert.Contains(t, err.Error(), "node:join refused")
	assert.Contains(t, err.Error(), "ovh_dedicated_server.example_plat_control_0")
}

func TestClassify_DestructiveUpdate_AutoApproveWithoutFlag_Refuses(t *testing.T) {
	j := &Join{AutoApprove: true, AllowDestructiveUpdate: false}
	s := &provisioner.PlanSummary{
		Changes: 1,
		DestructiveUpdates: []provisioner.ResourceImpact{
			{Address: "ovh_dedicated_server.example_plat_control_0", Type: "ovh_dedicated_server", Name: "example_plat_control_0", ChangedAttrs: []string{"os"}},
		},
	}
	p := &fakePrompter{}
	proceed, err := classifyAndConfirm(j, s, p)
	require.Error(t, err)
	assert.False(t, proceed)
	assert.Contains(t, err.Error(), "--allow-destructive-update")
	assert.Contains(t, err.Error(), "ovh_dedicated_server.example_plat_control_0")
	assert.Equal(t, 0, p.typedYesCalls, "no prompt under --auto-approve")
}

func TestClassify_DestructiveUpdate_AutoApproveWithFlag_Proceeds(t *testing.T) {
	j := &Join{AutoApprove: true, AllowDestructiveUpdate: true}
	s := &provisioner.PlanSummary{
		Changes: 1,
		DestructiveUpdates: []provisioner.ResourceImpact{
			{Address: "ovh_dedicated_server.example_plat_control_0", ChangedAttrs: []string{"os"}},
		},
	}
	p := &fakePrompter{}
	proceed, err := classifyAndConfirm(j, s, p)
	require.NoError(t, err)
	assert.True(t, proceed)
	assert.Equal(t, 0, p.typedYesCalls)
	assert.Equal(t, 0, p.yesNoCalls)
}

func TestClassify_DestructiveUpdate_Interactive_NeedsTypedYes(t *testing.T) {
	j := &Join{}
	s := &provisioner.PlanSummary{
		Changes: 1,
		DestructiveUpdates: []provisioner.ResourceImpact{
			{Address: "ovh_dedicated_server.example_plat_control_0", ChangedAttrs: []string{"os", "customizations"}},
		},
	}
	p := &fakePrompter{typedAnswer: true}
	proceed, err := classifyAndConfirm(j, s, p)
	require.NoError(t, err)
	assert.True(t, proceed)
	assert.Equal(t, 1, p.typedYesCalls)
	assert.Equal(t, 0, p.yesNoCalls)
}

func TestClassify_DestructiveUpdate_Interactive_TypedNoBlocks(t *testing.T) {
	j := &Join{}
	s := &provisioner.PlanSummary{
		Changes: 1,
		DestructiveUpdates: []provisioner.ResourceImpact{
			{Address: "ovh_dedicated_server.example_plat_control_0", ChangedAttrs: []string{"os"}},
		},
	}
	p := &fakePrompter{typedAnswer: false}
	proceed, err := classifyAndConfirm(j, s, p)
	require.NoError(t, err)
	assert.False(t, proceed)
	assert.Equal(t, 1, p.typedYesCalls)
}

func TestClassify_DestructiveUpdate_Interactive_PromptError(t *testing.T) {
	j := &Join{}
	s := &provisioner.PlanSummary{
		Changes: 1,
		DestructiveUpdates: []provisioner.ResourceImpact{
			{Address: "x", ChangedAttrs: []string{"os"}},
		},
	}
	p := &fakePrompter{typedErr: errors.New("stdin closed")}
	proceed, err := classifyAndConfirm(j, s, p)
	require.Error(t, err)
	assert.False(t, proceed)
	assert.Contains(t, err.Error(), "stdin closed")
}
