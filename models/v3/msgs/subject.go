package msgs

import (
	"slices"

	"github.com/Azure/arn-sdk/models/v3/schema/types"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
)

// arm.ResourceID is implemented a singly linked list.  The head of the list represents the entire resource ID.  If rid.Parent is not nil,
// it points to an arm.ResourceID representing the next valid parent scope.  The tail of the list is always a shared root arm.ResourceID
// representing the tenant.
//
// As the linked list is only singly linked and we will want to walk it in reverse, we work with pre-prepared slices containing pointers to
// each of the nodes ([]*arm.ResourceID).

// asSlice returns a slice containing pointers to each of the nodes of rid.
func asSlice(rid *arm.ResourceID) (rids []*arm.ResourceID) {
	for ; rid != nil; rid = rid.Parent {
		rids = append(rids, rid)
	}

	return rids
}

// maxSharedPrefix returns in slice form the maximal arm.ResourceID which is a shared prefix of a and b.
func maxSharedPrefix(a, b []*arm.ResourceID) (results []*arm.ResourceID) {
	// We can find the maximal arm.ResourceID which is a shared prefix of a and b by walking the slices backwards comparing their scopes.
	// While the scopes match, we append the scope to results.  By definition, a[len(a)-1] and b[len(b)-1] match.  Note that a and b may
	// have a significant shared prefix without having the same length.

	for i := 1; len(a)-i >= 0 && len(b)-i >= 0; i++ {
		// We compare using String() because unfortunately isChild is unexported.  At least String() caches its result internally.
		if a[len(a)-i].String() == b[len(b)-i].String() {
			results = append(results, a[len(a)-i])
		} else {
			break
		}
	}

	slices.Reverse(results)

	return results
}

// subject returns in string form the maximal arm.ResourceID which is a shared prefix of all of the resources in res.  At least one
// *types.ResourceEvent must be passed to this function.
func subject(res []types.NotificationResource) string {
	if len(res) == 0 {
		return ""
	}
	max := asSlice(res[0].ArmResource.ResourceID())

	for i := 1; i < len(res); i++ {
		max = maxSharedPrefix(max, asSlice(res[i].ArmResource.ResourceID()))
	}

	if len(max) <= 1 { // only the tenant-level scope was shared or no scopes were shared
		return "/"
	}

	return max[0].String()
}
