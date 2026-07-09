package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newStatsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stats",
		Short: "Show cache storage and request stats",
		RunE: func(cmd *cobra.Command, _ []string) error {
			flagAPI, _ := cmd.Flags().GetString("api")
			client, err := resolveClient(flagAPI)
			if err != nil {
				return err
			}
			s, err := client.Stats(cmd.Context())
			if err != nil {
				return err
			}
			// Real /api/v1/stats has no hit_rate field — compute it from
			// hits/misses (like the dashboard hit-meter), div-by-zero guarded.
			total := s.Hits + s.Misses
			var rate float64
			if total > 0 {
				rate = float64(s.Hits) / float64(total)
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Storage used:   %s\n", humanBytes(s.StorageBytes))
			fmt.Fprintf(out, "Requests:       %d  (hits %d / misses %d)\n", s.Requests, s.Hits, s.Misses)
			fmt.Fprintf(out, "Hit rate:       %.1f%%\n", rate*100)
			return nil
		},
	}
}

// humanBytes renders a byte count as a short human-readable size.
// ponytail: 4 units (B/KiB/MiB/GiB/TiB) covers every realistic cache size;
// add PiB if someone actually self-hosts a petabyte cache.
func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGT"[exp])
}
