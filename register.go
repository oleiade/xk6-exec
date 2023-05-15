package exec

import (
	"github.com/oleiade/xk6-exec/exec"
	"go.k6.io/k6/js/modules"
)

func init() {
	modules.Register("k6/x/cmd", new(exec.RootModule))
}
