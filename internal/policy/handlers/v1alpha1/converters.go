package v1alpha1

import (
	apiv1alpha1 "github.com/dcm-project/control-plane/api/policy/v1alpha1"
)

// Type conversion helpers kept for clarity; api and server generated types are identical.
func policyServerToV1Alpha1(p apiv1alpha1.Policy) apiv1alpha1.Policy {
	return p
}

func policyV1Alpha1ToServer(p apiv1alpha1.Policy) apiv1alpha1.Policy {
	return p
}

func listResponseV1Alpha1ToServer(r apiv1alpha1.PolicyList) apiv1alpha1.PolicyList {
	return r
}
