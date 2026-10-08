// Package compose holds the compose project type of the API (ComposeApp).
package compose

import "github.com/compose-spec/compose-go/v2/types"

// Project is a compose-go project whose JSON keeps the top-level x-
// extensions (x-casaos), like compose-go v1 did. In v2,
// (*types.Project).MarshalJSON takes options, so it is no json.Marshaler
// any more and plain encoding drops the extensions.
type Project types.Project

func (p Project) MarshalJSON() ([]byte, error) {
	return (*types.Project)(&p).MarshalJSON()
}
