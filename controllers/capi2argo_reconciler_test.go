package controllers

import (
	"fmt"
)

func init() {
	fmt.Println("TODO: Tests for (*Capi2Argo).Reconcile()")
}

// TODO
// In here we will need somehow to mock Kubernetes.client in order to implement
// tests without needing an active cluster as a target.
//
// There are couple of ways of doing so:
// 		1. Create mocked objects that implement interface ctrl.Reconcile() so we can manipulate
//			 inputs and returns with a fake k8s client.
//		2. Utilize "envtest" package from Operator Framework that creates on the fly a
//			 fake k8s cluster on test runtime so we don't need to heavily alter our codebase.
