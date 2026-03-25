package telemetry

import (
	"context"
	"database/sql"
	"fmt"
)

// MonthlyInstall holds the install count for a single month.
type MonthlyInstall struct {
	Month    string `json:"month"`
	Installs int    `json:"installs"`
}

// GetMonthlyInstalls returns up to 36 months of install counts for a package, ordered ascending.
func GetMonthlyInstalls(ctx context.Context, db *sql.DB, packageID int64) ([]MonthlyInstall, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT month, installs FROM (
			SELECT month, installs
			FROM monthly_installs
			WHERE package_id = ?
			ORDER BY month DESC
			LIMIT 36
		) ORDER BY month ASC`, packageID)
	if err != nil {
		return nil, fmt.Errorf("querying monthly installs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var result []MonthlyInstall
	for rows.Next() {
		var m MonthlyInstall
		if err := rows.Scan(&m.Month, &m.Installs); err != nil {
			return nil, fmt.Errorf("scanning monthly install row: %w", err)
		}
		result = append(result, m)
	}
	return result, rows.Err()
}
