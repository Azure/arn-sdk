// Code generated by "stringer -type=ResourcesContainer -linecomment"; DO NOT EDIT.

package types

import "strconv"

func _() {
	// An "invalid array index" compiler error signifies that the constant values have changed.
	// Re-run the stringer command to generate them again.
	var x [1]struct{}
	_ = x[RCUnknown-0]
	_ = x[RCInline-1]
	_ = x[RCBlob-2]
}

const _ResourcesContainer_name = "\"\"\"inline\"\"blob\""

var _ResourcesContainer_index = [...]uint8{0, 2, 10, 16}

func (i ResourcesContainer) String() string {
	if i >= ResourcesContainer(len(_ResourcesContainer_index)-1) {
		return "ResourcesContainer(" + strconv.FormatInt(int64(i), 10) + ")"
	}
	return _ResourcesContainer_name[_ResourcesContainer_index[i]:_ResourcesContainer_index[i+1]]
}
