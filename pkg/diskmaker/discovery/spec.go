package discovery

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"time"

	"github.com/openshift/local-storage-operator/pkg/apis/local/v1alpha1"
	"github.com/pkg/errors"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	apiTypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/klog"
)

// newDiscoveryResultInstance creates spec for the LocalVolumeDiscoveryResult
func newDiscoveryResultInstance(nodeName, namespace, parentObjName, parentObjUID string) *v1alpha1.LocalVolumeDiscoveryResult {
	truncatedNodeName := truncateNodeName(resultCRName, nodeName)
	labels := map[string]string{}
	labels[resultCRLabel] = nodeName
	cr := &v1alpha1.LocalVolumeDiscoveryResult{
		ObjectMeta: metav1.ObjectMeta{
			Name:      truncatedNodeName,
			Namespace: namespace,
			Labels:    labels,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: v1alpha1.SchemeGroupVersion.String(),
					Kind:       "LocalVolumeDiscovery",
					Name:       parentObjName,
					UID:        apiTypes.UID(parentObjUID),
				},
			},
		},
		Spec: v1alpha1.LocalVolumeDiscoveryResultSpec{
			NodeName: nodeName,
		},
	}

	return cr
}

// ensureDiscoveryResultCR creates a new LocalVolumeDiscoveryResult custome resource on the node, if not present
func (discovery *DeviceDiscovery) ensureDiscoveryResultCR() error {
	nodeName := os.Getenv("MY_NODE_NAME")
	namespace := os.Getenv("WATCH_NAMESPACE")
	parentObjUID := os.Getenv("UID")
	parentObjName := os.Getenv("POD_NAME")
	if nodeName == "" || namespace == "" || parentObjUID == "" || parentObjName == "" {
		return errors.New("failed to create LocalVolumeDiscoveryResult resource. missing required env variables")
	}
	resultCR := newDiscoveryResultInstance(nodeName, namespace, parentObjName, parentObjUID)
	existing := v1alpha1.LocalVolumeDiscoveryResult{}
	err := discovery.client.Get(context.TODO(), types.NamespacedName{Name: resultCR.Name, Namespace: namespace}, &existing)
	switch {
	case err == nil:
		existing.ObjectMeta.OwnerReferences = resultCR.ObjectMeta.OwnerReferences
		resultCR.ObjectMeta = existing.ObjectMeta
		err = discovery.client.Update(context.TODO(), resultCR)
		if err != nil {
			return errors.Wrapf(err, "failed to update LocalVolumeDiscoveryResult resource")
		}
		klog.Info("successfully updated LocalVolumeDiscoveryResult resource")
	case kerrors.IsNotFound(err):
		err = discovery.client.Create(context.TODO(), resultCR)
		if err != nil {
			return errors.Wrapf(err, "failed to create LocalVolumeDiscoveryResult resource")
		}
		klog.Info("successfully created LocalVolumeDiscoveryResult resource")
	}

	return nil
}

// updateStatus updates the LocalVolumeDiscoveryResult resource status
func (discovery *DeviceDiscovery) updateStatus() error {
	resultCR := v1alpha1.LocalVolumeDiscoveryResult{}
	truncatedNodeName := truncateNodeName(resultCRName, os.Getenv("MY_NODE_NAME"))
	err := discovery.client.Get(context.TODO(), types.NamespacedName{Name: truncatedNodeName,
		Namespace: os.Getenv("WATCH_NAMESPACE")}, &resultCR)
	if err != nil {
		if kerrors.IsNotFound(err) {
			klog.Warning("result resource not found. Ignoring since object must be deleted.")
			return nil
		}
		return errors.Wrapf(err, "failed to retrieve LocalVolumeDiscoveryResult resource to update status")
	}

	// Update discovered devce list and discovery time
	resultCR.Status.DiscoveredDevices = discovery.disks
	resultCR.Status.DiscoveredTimeStamp = time.Now().UTC().Format(time.RFC3339)

	err = discovery.client.Status().Update(context.TODO(), resultCR.DeepCopyObject())
	if err != nil {
		return errors.Wrapf(err, "failed to update the device status in the LocalVolumeDiscoveryResult resource")
	}

	return nil
}

// hash stableName computes a stable pseudorandom string suitable for inclusion in a Kubernetes object name from the given seed string.
func hash(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:16])
}

// truncateNodeName hashes the nodeName in case it would case the name to be longer than 63 characters
func truncateNodeName(format, nodeName string) string {
	if len(nodeName)+len(fmt.Sprintf(format, "")) > validation.DNS1035LabelMaxLength {
		hashed := hash(nodeName)
		klog.Infof("format and nodeName longer than %d chars, nodeName %s will be %s", validation.DNS1035LabelMaxLength, nodeName, hashed)
		nodeName = hashed
	}
	return fmt.Sprintf(format, nodeName)
}
