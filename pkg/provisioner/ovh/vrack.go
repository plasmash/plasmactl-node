package ovh

import (
	"context"
	"fmt"
	"time"
)

// VRackReconciler ensures a server's private interface (identified by MAC
// address) is a member of the platform's vRack. It is idempotent: when the
// VNI is already in the vRack, Reconcile is a no-op.
//
// Implements provisioner.VRackReconciler via the adapter in
// pkg/provisioner/factory_ovh_vrack.go.
//
// OVH vRack membership model (verified against eu.api.ovh.com/1.0/vrack.json
// and eu.api.ovh.com/1.0/dedicated/server.json, 2026-04-29):
//
//   - Membership is binary: a VirtualNetworkInterface (VNI) UUID is either in
//     the vRack or not. There is no per-member VLAN stored in the vRack API.
//   - VLAN configuration (e.g. "tag this interface with VLAN 7") is an
//     OS-level concern handled by network configuration templates, not by
//     OVH's API.
//   - The vlanID parameter accepted by Reconcile is intentionally unused here.
//     It is passed for interface uniformity so the caller (node:join) can carry
//     the VLAN ID through to the OS templating step without a separate lookup.
//   - The MAC address is used only to resolve the VNI UUID via:
//       GET /dedicated/server/{serverID}/virtualNetworkInterface → []uuid
//       GET /dedicated/server/{serverID}/virtualNetworkInterface/{uuid}
//         → {networkInterfaceController: []macAddress, ...}
type VRackReconciler struct {
	Client *Client
}

// Reconcile finds the vRack whose description or name matches platformName,
// resolves the server's private-interface VNI UUID from privateMAC, then:
//   - if the VNI is not yet a member of the vRack, POSTs membership
//   - if the VNI is already a member, no-op
//
// Errors when zero or multiple vRacks match platformName, or when no VNI on
// the server has the given MAC address.
func (r *VRackReconciler) Reconcile(ctx context.Context, platformName string, vlanID int, serverID, privateMAC string) error {
	serviceName, err := r.findVRackByPlatformName(ctx, platformName)
	if err != nil {
		return err
	}

	vniUUID, err := r.resolveVNIByMAC(ctx, serverID, privateMAC)
	if err != nil {
		return err
	}

	isMember, err := r.isVNIMember(ctx, serviceName, vniUUID)
	if err != nil {
		return err
	}

	if isMember {
		return nil // already in the correct vRack — nothing to do
	}
	return r.addMember(ctx, serviceName, vniUUID)
}

// findVRackByPlatformName enumerates vRacks and returns the single one whose
// description or name matches platformName. The OVH API exposes both fields
// independently; the OVH Manager UI surfaces them as separate columns. Older
// vRacks tend to use `description`; newer ones use `name`. Match on either.
// Errors on zero or multiple matches.
func (r *VRackReconciler) findVRackByPlatformName(ctx context.Context, platformName string) (string, error) {
	var serviceNames []string
	if err := r.Client.GetJSON(ctx, "/vrack", &serviceNames); err != nil {
		return "", fmt.Errorf("list vracks: %w", err)
	}
	var matches []string
	for _, sn := range serviceNames {
		var detail struct {
			Description string `json:"description"`
			Name        string `json:"name"`
		}
		if err := r.Client.GetJSON(ctx, "/vrack/"+sn, &detail); err != nil {
			return "", fmt.Errorf("describe vrack %s: %w", sn, err)
		}
		if detail.Description == platformName || detail.Name == platformName {
			matches = append(matches, sn)
		}
	}
	switch len(matches) {
	case 0:
		return "", fmt.Errorf("no vRack with description or name %q in this OVH account (found %d vRacks total)", platformName, len(serviceNames))
	case 1:
		return matches[0], nil
	default:
		return "", fmt.Errorf("multiple vRacks (%v) match description or name %q — ambiguous", matches, platformName)
	}
}

// resolveVNIByMAC lists the server's VirtualNetworkInterfaces and returns the
// UUID of the one whose networkInterfaceController list contains privateMAC.
// Errors if no match is found.
func (r *VRackReconciler) resolveVNIByMAC(ctx context.Context, serverID, privateMAC string) (string, error) {
	var uuids []string
	if err := r.Client.GetJSON(ctx, "/dedicated/server/"+serverID+"/virtualNetworkInterface?mode=vrack", &uuids); err != nil {
		return "", fmt.Errorf("list VNIs for %s: %w", serverID, err)
	}
	for _, uuid := range uuids {
		var vni struct {
			UUID                     string   `json:"uuid"`
			NetworkInterfaceController []string `json:"networkInterfaceController"`
		}
		if err := r.Client.GetJSON(ctx, "/dedicated/server/"+serverID+"/virtualNetworkInterface/"+uuid, &vni); err != nil {
			return "", fmt.Errorf("describe VNI %s on %s: %w", uuid, serverID, err)
		}
		for _, mac := range vni.NetworkInterfaceController {
			if mac == privateMAC {
				return uuid, nil
			}
		}
	}
	return "", fmt.Errorf("no vrack-mode VNI with MAC %s found on server %s (checked %d VNIs)", privateMAC, serverID, len(uuids))
}

// isVNIMember returns true if vniUUID is in the vRack's member list.
func (r *VRackReconciler) isVNIMember(ctx context.Context, serviceName, vniUUID string) (bool, error) {
	var members []string
	if err := r.Client.GetJSON(ctx, "/vrack/"+serviceName+"/dedicatedServerInterface", &members); err != nil {
		return false, fmt.Errorf("list vRack members: %w", err)
	}
	for _, m := range members {
		if m == vniUUID {
			return true, nil
		}
	}
	return false, nil
}

// addMember POSTs the VNI UUID into the vRack and waits for the resulting
// task to complete.
func (r *VRackReconciler) addMember(ctx context.Context, serviceName, vniUUID string) error {
	body := map[string]any{
		"dedicatedServerInterface": vniUUID,
	}
	var resp struct {
		ID     int64  `json:"id"`
		Status string `json:"status"`
	}
	if err := r.Client.PostJSON(ctx, "/vrack/"+serviceName+"/dedicatedServerInterface", body, &resp); err != nil {
		return fmt.Errorf("add VNI %s to vRack %s: %w", vniUUID, serviceName, err)
	}
	return r.waitForTask(ctx, serviceName, resp.ID)
}

// removeMember DELETEs the VNI UUID from the vRack and waits for the task.
// Not used during normal Reconcile (membership is binary — no wrong-VLAN case
// at the vRack level), but retained for completeness if future callers need
// explicit removal.
func (r *VRackReconciler) removeMember(ctx context.Context, serviceName, vniUUID string) error {
	var resp struct {
		ID     int64  `json:"id"`
		Status string `json:"status"`
	}
	if err := r.Client.DeleteJSON(ctx, "/vrack/"+serviceName+"/dedicatedServerInterface/"+vniUUID, &resp); err != nil {
		return fmt.Errorf("remove VNI %s from vRack %s: %w", vniUUID, serviceName, err)
	}
	return r.waitForTask(ctx, serviceName, resp.ID)
}

// waitForTask polls until OVH reports the task "done" or "cancelled".
// Backoff: starts at 2 s, increases by 2 s each iteration, caps at 10 s.
// Total budget: 3 minutes (typical vRack task: 30–90 s).
func (r *VRackReconciler) waitForTask(ctx context.Context, serviceName string, taskID int64) error {
	if taskID == 0 {
		return nil // caller synthesized a task with no ID — nothing to poll
	}
	deadline := time.Now().Add(3 * time.Minute)
	delay := 2 * time.Second
	for {
		var status struct {
			Status string `json:"status"`
		}
		path := fmt.Sprintf("/vrack/%s/task/%d", serviceName, taskID)
		if err := r.Client.GetJSON(ctx, path, &status); err != nil {
			return fmt.Errorf("poll vrack task %d: %w", taskID, err)
		}
		switch status.Status {
		case "done":
			return nil
		case "cancelled":
			return fmt.Errorf("vrack task %d was cancelled", taskID)
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("vrack task %d timed out (last status %q)", taskID, status.Status)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
		if delay < 10*time.Second {
			delay += 2 * time.Second
		}
	}
}
