// Code generated by "stringer -type=DataBoundary -linecomment"; DO NOT EDIT.

package types

import "strconv"

func _() {
	// An "invalid array index" compiler error signifies that the constant values have changed.
	// Re-run the stringer command to generate them again.
	var x [1]struct{}
	_ = x[DBUnknown-0]
	_ = x[DBGlobal-1]
	_ = x[DBEU-2]
}

const _DataBoundary_name = "\"\"\"global\"\"eu\""

var _DataBoundary_index = [...]uint8{0, 2, 10, 14}

func (i DataBoundary) String() string {
	if i >= DataBoundary(len(_DataBoundary_index)-1) {
		return "DataBoundary(" + strconv.FormatInt(int64(i), 10) + ")"
	}
	return _DataBoundary_name[_DataBoundary_index[i]:_DataBoundary_index[i+1]]
}
