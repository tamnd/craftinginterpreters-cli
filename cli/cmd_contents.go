package cli

import (
	"github.com/spf13/cobra"
)

func (a *App) contentsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "contents",
		Short: "List all chapters from the table of contents",
		RunE: func(cmd *cobra.Command, _ []string) error {
			n := a.effectiveLimit(0)
			a.progressf("fetching table of contents...")
			chapters, err := a.client.Contents(cmd.Context(), n)
			if err != nil {
				return mapFetchErr(err)
			}
			return a.renderOrEmpty(chapters, len(chapters))
		},
	}
	return cmd
}
