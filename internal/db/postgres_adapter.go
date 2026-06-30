package db

import (
	"fmt"
)

// PostgresAdapter implements the Adapter interface for PostgreSQL databases.
//
// Execution model: databases are external (managed RDS) reached from inside an
// application pod, which has no psql CLI but does ship asyncpg in its venv. SQL
// runs through pgExecScript. The connection password is NEVER in the argv: either
// the script reads the connection URL from os.environ (env mode) or receives the
// credentials via stdin (stdin mode).
type PostgresAdapter struct{}

func (a *PostgresAdapter) Type() DatabaseType {
	return PostgreSQL
}

// pgExecScript connects via asyncpg and prints {"columns":[...],"rows":[[...]]}.
// argv: mode sql   (mode="env": argv also has [envVar]; mode="stdin": creds JSON on stdin)
//   - env mode:   argv = ["env", connEnvVar, sql]; URL from os.environ[connEnvVar]
//   - stdin mode: argv = ["stdin", sql]; {host,port,user,password,database} JSON on stdin
const pgExecScript = `
import sys, os, json, asyncio, asyncpg
mode = sys.argv[1]
if mode == "env":
    raw = os.environ.get(sys.argv[2], "")
    sql = sys.argv[3]
    # strip SQLAlchemy "+driver" suffix (e.g. postgresql+asyncpg://)
    if "://" in raw:
        scheme, rest = raw.split("://", 1)
        raw = scheme.split("+")[0] + "://" + rest
    conn = {"dsn": raw}
else:
    sql = sys.argv[2]
    c = json.load(sys.stdin)
    conn = {"host": c["host"], "port": int(c["port"]), "user": c["user"],
            "password": c["password"], "database": c["database"]}
async def main():
    con = await asyncpg.connect(**conn)
    try:
        rows = await con.fetch(sql)
    finally:
        await con.close()
    cols = list(rows[0].keys()) if rows else []
    out = [["NULL" if v is None else str(v) for v in r.values()] for r in rows]
    sys.stdout.write(json.dumps({"columns": cols, "rows": out}))
asyncio.run(main())
`

func (a *PostgresAdapter) BuildCommand(sql string, creds Credentials) ([]string, []byte) {
	return buildPythonCommand(creds, pgExecScript, sql)
}

func (a *PostgresAdapter) ParseOutput(raw []byte) (*QueryResult, error) {
	return parseJSONResult(raw)
}

func (a *PostgresAdapter) ListTablesSQL(database string) string {
	return "SELECT table_name FROM information_schema.tables " +
		"WHERE table_schema = 'public' AND table_type = 'BASE TABLE'"
}

func (a *PostgresAdapter) DescribeTableSQL(database, table string) string {
	t := sanitizeSQLIdentifier(table)
	return fmt.Sprintf(
		"SELECT column_name, data_type, is_nullable, "+
			"CASE WHEN pk.column_name IS NOT NULL THEN 'PRI' ELSE '' END AS column_key, "+
			"column_default "+
			"FROM information_schema.columns c "+
			"LEFT JOIN ("+
			"SELECT ku.column_name FROM information_schema.table_constraints tc "+
			"JOIN information_schema.key_column_usage ku ON tc.constraint_name = ku.constraint_name "+
			"WHERE tc.table_schema = 'public' AND tc.table_name = '%s' AND tc.constraint_type = 'PRIMARY KEY'"+
			") pk ON c.column_name = pk.column_name "+
			"WHERE c.table_schema = 'public' AND c.table_name = '%s' "+
			"ORDER BY c.ordinal_position",
		t, t,
	)
}

func (a *PostgresAdapter) ForeignKeysSQL(database string) string {
	return "SELECT kcu.table_name, kcu.column_name, " +
		"ccu.table_name AS referenced_table_name, ccu.column_name AS referenced_column_name " +
		"FROM information_schema.table_constraints tc " +
		"JOIN information_schema.key_column_usage kcu ON tc.constraint_name = kcu.constraint_name " +
		"JOIN information_schema.constraint_column_usage ccu ON tc.constraint_name = ccu.constraint_name " +
		"WHERE tc.table_schema = 'public' AND tc.constraint_type = 'FOREIGN KEY'"
}

func (a *PostgresAdapter) DetectCLI() []string {
	// Probe the python interpreter rather than psql (pods have no psql).
	return []string{"which", "python3"}
}
