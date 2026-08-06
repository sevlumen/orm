package ormcli

import (
	"flag"
	"fmt"
)

const versionUsage = `Usage: orm version [options]

Options:
  --json   Emit a versioned JSON result.
`

func (app *App) runVersion(args []string) error {
	var jsonOutput bool
	_, err := parseFlags("version", versionUsage, args, func(set *flag.FlagSet) {
		set.BoolVar(&jsonOutput, "json", false, "emit JSON")
	})
	if err != nil {
		return err
	}
	info := app.BuildInfo()
	human := fmt.Sprintf(
		"orm %s commit=%s date=%s dirty=%t go=%s %s/%s\n",
		info.Version,
		info.Commit,
		info.Date,
		info.Dirty,
		info.GoVersion,
		info.GOOS,
		info.GOARCH,
	)
	return app.writeResult(jsonOutput, "version", info, human)
}
