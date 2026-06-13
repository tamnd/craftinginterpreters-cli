package cli

import (
	"github.com/spf13/cobra"
)

func (a *App) searchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search chapters by title",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			n := a.effectiveLimit(0)
			query := args[0]
			a.progressf("searching for %q...", query)
			hits, err := a.client.Search(cmd.Context(), query, n)
			if err != nil {
				return mapFetchErr(err)
			}
			return a.renderOrEmpty(hits, len(hits))
		},
	}
	return cmd
}
