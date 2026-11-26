// Appwrite Function entrypoint (Open Runtimes Go contract).
//
// The runtime expects `package handler` with a `Main` function; see
// internal/fn for the adapter that replays each invocation against the same
// http.Handler the local server (cmd/api) uses.
package handler

import (
	types "github.com/open-runtimes/types-for-go/v4/openruntimes"

	"github.com/danielvm-git/appwrite-canary-go-mysql/internal/fn"
)

func Main(ctx types.Context) types.Response {
	return fn.Handle(ctx)
}
