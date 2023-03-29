package sqlstore

import (
	"bytes"
	"fmt"
	"io"

	sq "github.com/Masterminds/squirrel"
	"github.com/mattermost/focalboard/server/model"
	"github.com/mattermost/mattermost-server/v6/shared/mlog"
	"github.com/mattermost/morph/models"
)

// EnsureSchemaMigrationFormat checks the schema migrations table
// format and, if it's not using the new shape, it migrates the old
// one's status before initializing the migrations engine.
func (s *SQLStore) EnsureSchemaMigrationFormat() error {
	migrationNeeded, err := s.isSchemaMigrationNeeded()
	if err != nil {
		return err
	}

	if !migrationNeeded {
		return nil
	}

	s.logger.Info("Migrating schema migration to new format")

	legacySchemaVersion, err := s.getLegacySchemaVersion()
	if err != nil {
		return err
	}

	migrations, err := getEmbeddedMigrations()
	if err != nil {
		return err
	}
	filteredMigrations := filterMigrations(migrations, legacySchemaVersion)

	if err := s.createTempSchemaTable(); err != nil {
		return err
	}

	s.logger.Info("Populating the temporal schema table", mlog.Uint32("legacySchemaVersion", legacySchemaVersion), mlog.Int("migrations", len(filteredMigrations)))

	if err := s.populateTempSchemaTable(filteredMigrations); err != nil {
		return err
	}

	if err := s.useNewSchemaTable(); err != nil {
		return err
	}

	return nil
}

// getEmbeddedMigrations returns a list of the embedded migrations
// using the morph migration format. The migrations do not have the
// contents set, as the goal is to obtain a list of them.
func getEmbeddedMigrations() ([]*models.Migration, error) {
	assetsList, err := Assets.ReadDir("migrations")
	if err != nil {
		return nil, err
	}

	migrations := []*models.Migration{}
	for _, f := range assetsList {
		m, err := models.NewMigration(io.NopCloser(&bytes.Buffer{}), f.Name())
		if err != nil {
			return nil, err
		}

		if m.Direction != models.Up {
			continue
		}

		migrations = append(migrations, m)
	}

	return migrations, nil
}

// filterMigrations takes the whole list of migrations parsed from the
// embedded directory and returns a filtered list that only contains
// one migration per version and those migrations that have already
// run based on the legacySchemaVersion.
func filterMigrations(migrations []*models.Migration, legacySchemaVersion uint32) []*models.Migration {
	filteredMigrations := []*models.Migration{}
	for _, migration := range migrations {
		// we only take into account up migrations to avoid duplicates
		if migration.Direction != models.Up {
			continue
		}

		// we're only interested on registering migrations that
		// already run, so we skip those above the legacy version
		if migration.Version > legacySchemaVersion {
			continue
		}

		filteredMigrations = append(filteredMigrations, migration)
	}

	return filteredMigrations
}

func (s *SQLStore) isSchemaMigrationNeeded() (bool, error) {
	// Check if `dirty` column exists on schema version table.
	// This column exists only for the old schema version table.

	// SQLite needs a bit of a special handling
	if s.dbType == model.SqliteDBType {
		return s.isSchemaMigrationNeededSQLite()
	}

	query := s.getQueryBuilder(s.db).
		Select("count(*)").
		From("information_schema.COLUMNS").
		Where(sq.Eq{
			"TABLE_NAME":  s.tablePrefix + "schema_migrations",
			"COLUMN_NAME": "dirty",
		})

	switch s.dbType {
	case model.MysqlDBType:
		query = query.Where(sq.Eq{"TABLE_SCHEMA": s.schemaName})
	case model.PostgresDBType:
		query = query.Where(sq.Eq{"TABLE_SCHEMA": "current_schema()"})
	}

	row := query.QueryRow()

	var count int
	if err := row.Scan(&count); err != nil {
		s.logger.Error("failed to check for columns of schema_migrations table", mlog.Err(err))
		return false, err
	}

	return count == 1, nil
}

func (s *SQLStore) isSchemaMigrationNeededSQLite() (bool, error) {
	// the way to check presence of a column is different
	// for SQLite. Hence, the separate function

	query := fmt.Sprintf("PRAGMA table_info(\"%sschema_migrations\");", s.tablePrefix)
	rows, err := s.db.Query(query)
	if err != nil {
		s.logger.Error("SQLite - failed to check for columns in schema_migrations table", mlog.Err(err))
		return false, err
	}

	defer s.CloseRows(rows)

	data := [][]*string{}
	for rows.Next() {
		// PRAGMA returns 6 columns
		row := make([]*string, 6)

		err := rows.Scan(
			&row[0],
			&row[1],
			&row[2],
			&row[3],
			&row[4],
			&row[5],
		)
		if err != nil {
			s.logger.Error("error scanning rows from SQLite schema_migrations table definition", mlog.Err(err))
			return false, err
		}

		data = append(data, row)
	}

	nameColumnFound := false
	for _, row := range data {
		if len(row) >= 2 && *row[1] == "dirty" {
			nameColumnFound = true
			break
		}
	}

	return nameColumnFound, nil
}

func (s *SQLStore) getLegacySchemaVersion() (uint32, error) {
	query := s.getQueryBuilder(s.db).
		Select("version").
		From(s.tablePrefix + "schema_migrations")

	row := query.QueryRow()

	var version uint32
	if err := row.Scan(&version); err != nil {
		s.logger.Error("error fetching legacy schema version", mlog.Err(err))
		return version, err
	}

	return version, nil
}

func (s *SQLStore) createTempSchemaTable() error {
	// squirrel doesn't support DDL query in query builder
	// so, we need to use a plain old string
	query := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (Version bigint NOT NULL, Name varchar(64) NOT NULL, PRIMARY KEY (Version))", s.tablePrefix+tempSchemaMigrationTableName)
	if _, err := s.db.Exec(query); err != nil {
		s.logger.Error("failed to create temporary schema migration table", mlog.Err(err))
		s.logger.Error("createTempSchemaTable error  " + err.Error())
		return err
	}

	return nil
}

func (s *SQLStore) populateTempSchemaTable(migrations []*models.Migration) error {
	query := s.getQueryBuilder(s.db).
		Insert(s.tablePrefix+tempSchemaMigrationTableName).
		Columns("Version", "Name")

	for _, migration := range migrations {
		s.logger.Info("-- Registering migration", mlog.Uint32("version", migration.Version), mlog.String("name", migration.Name))
		query = query.Values(migration.Version, migration.Name)
	}

	if _, err := query.Exec(); err != nil {
		s.logger.Error("failed to insert migration records into temporary schema table", mlog.Err(err))
		return err
	}

	return nil
}

func (s *SQLStore) useNewSchemaTable() error {
	// first delete the old table, then
	// rename the new table to old table's name

	// renaming old schema migration table. Will delete later once the migration is
	// complete, just in case.
	var query string
	if s.dbType == model.MysqlDBType {
		query = fmt.Sprintf("RENAME TABLE `%sschema_migrations` TO `%sschema_migrations_old_temp`", s.tablePrefix, s.tablePrefix)
	} else {
		query = fmt.Sprintf("ALTER TABLE %sschema_migrations RENAME TO %sschema_migrations_old_temp", s.tablePrefix, s.tablePrefix)
	}

	if _, err := s.db.Exec(query); err != nil {
		s.logger.Error("failed to rename old schema migration table", mlog.Err(err))
		return err
	}

	// renaming new temp table to old table's name
	if s.dbType == model.MysqlDBType {
		query = fmt.Sprintf("RENAME TABLE `%s%s` TO `%sschema_migrations`", s.tablePrefix, tempSchemaMigrationTableName, s.tablePrefix)
	} else {
		query = fmt.Sprintf("ALTER TABLE %s%s RENAME TO %sschema_migrations", s.tablePrefix, tempSchemaMigrationTableName, s.tablePrefix)
	}

	if _, err := s.db.Exec(query); err != nil {
		s.logger.Error("failed to rename temp schema table", mlog.Err(err))
		return err
	}

	return nil
}

func (s *SQLStore) deleteOldSchemaMigrationTable() error {
	query := "DROP TABLE IF EXISTS " + s.tablePrefix + "schema_migrations_old_temp"
	if _, err := s.db.Exec(query); err != nil {
		s.logger.Error("failed to delete old temp schema migrations table", mlog.Err(err))
		return err
	}

	return nil
}
