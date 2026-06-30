package db

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// MySQLAdapter implements the Adapter interface for MySQL databases.
//
// Execution model mirrors PostgresAdapter: the database is external (managed
// RDS) reached from inside an application pod that ships pymysql in its venv but
// has no mysql CLI. The password is NEVER in the argv — the script reads the
// connection URL from os.environ (env mode) or credentials from stdin (stdin mode).
type MySQLAdapter struct{}

func (a *MySQLAdapter) Type() DatabaseType {
	return MySQL
}

// mysqlExecScript connects via pymysql and prints {"columns":[...],"rows":[[...]]}.
//   - env mode:   argv = ["env", connEnvVar, sql]; URL parsed from os.environ[connEnvVar]
//   - stdin mode: argv = ["stdin", sql]; {host,port,user,password,database} JSON on stdin
const mysqlExecScript = `
import sys, os, json, pymysql
from urllib.parse import urlparse, unquote
mode = sys.argv[1]
if mode == "env":
    raw = os.environ.get(sys.argv[2], "")
    sql = sys.argv[3]
    if "://" in raw:  # strip "+aiomysql" etc.
        scheme, rest = raw.split("://", 1)
        raw = scheme.split("+")[0] + "://" + rest
    u = urlparse(raw)
    conn = dict(host=u.hostname, port=u.port or 3306, user=unquote(u.username or ""),
                password=unquote(u.password or ""), database=(u.path or "/").lstrip("/") or None)
else:
    sql = sys.argv[2]
    c = json.load(sys.stdin)
    conn = dict(host=c["host"], port=int(c["port"]), user=c["user"],
                password=c["password"], database=c["database"] or None)
con = pymysql.connect(**conn)
try:
    cur = con.cursor()
    cur.execute(sql)
    cols = [d[0] for d in cur.description] if cur.description else []
    rows = [["NULL" if v is None else str(v) for v in r] for r in cur.fetchall()]
finally:
    con.close()
sys.stdout.write(json.dumps({"columns": cols, "rows": rows}))
`

func (a *MySQLAdapter) BuildCommand(sql string, creds Credentials) ([]string, []byte) {
	return buildPythonCommand(creds, mysqlExecScript, sql)
}

func (a *MySQLAdapter) ParseOutput(raw []byte) (*QueryResult, error) {
	return parseJSONResult(raw)
}

func (a *MySQLAdapter) ListTablesSQL(database string) string {
	return fmt.Sprintf(
		"SELECT TABLE_NAME FROM INFORMATION_SCHEMA.TABLES WHERE TABLE_SCHEMA = '%s' AND TABLE_TYPE = 'BASE TABLE'",
		sanitizeSQLIdentifier(database),
	)
}

func (a *MySQLAdapter) DescribeTableSQL(database, table string) string {
	return fmt.Sprintf(
		"SELECT COLUMN_NAME, DATA_TYPE, IS_NULLABLE, COLUMN_KEY, COLUMN_DEFAULT "+
			"FROM INFORMATION_SCHEMA.COLUMNS "+
			"WHERE TABLE_SCHEMA = '%s' AND TABLE_NAME = '%s' "+
			"ORDER BY ORDINAL_POSITION",
		sanitizeSQLIdentifier(database), sanitizeSQLIdentifier(table),
	)
}

func (a *MySQLAdapter) ForeignKeysSQL(database string) string {
	return fmt.Sprintf(
		"SELECT kcu.TABLE_NAME, kcu.COLUMN_NAME, kcu.REFERENCED_TABLE_NAME, kcu.REFERENCED_COLUMN_NAME "+
			"FROM INFORMATION_SCHEMA.KEY_COLUMN_USAGE kcu "+
			"JOIN INFORMATION_SCHEMA.REFERENTIAL_CONSTRAINTS rc "+
			"ON kcu.CONSTRAINT_NAME = rc.CONSTRAINT_NAME AND kcu.TABLE_SCHEMA = rc.CONSTRAINT_SCHEMA "+
			"WHERE kcu.TABLE_SCHEMA = '%s' AND kcu.REFERENCED_TABLE_NAME IS NOT NULL",
		sanitizeSQLIdentifier(database),
	)
}

func (a *MySQLAdapter) DetectCLI() []string {
	// Probe the python interpreter rather than mysql (pods have no mysql CLI).
	return []string{"which", "python3"}
}

// sanitizeUTF8 replaces non-UTF-8 bytes with "?".
func sanitizeUTF8(raw []byte) string {
	if utf8.Valid(raw) {
		return string(raw)
	}
	var b strings.Builder
	b.Grow(len(raw))
	for len(raw) > 0 {
		r, size := utf8.DecodeRune(raw)
		if r == utf8.RuneError && size == 1 {
			b.WriteByte('?')
		} else {
			b.WriteRune(r)
		}
		raw = raw[size:]
	}
	return b.String()
}
