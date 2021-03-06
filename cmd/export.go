package cmd

import (
	"bufio"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"strings"

	_ "github.com/go-sql-driver/mysql"
	"github.com/spf13/cobra"
)

type exportOpt struct {
	db        tidbAccessOptions
	mode      string
	queryFile string
	dir       string
	tables    []string
}

func newExportCmd() *cobra.Command {
	var opt exportOpt
	cmd := &cobra.Command{
		Use:   "export",
		Short: "export queries, schemas and statistic information from TiDB",
		Long:  `export queries, schemas and statistic information from TiDB`,
		RunE: func(cmd *cobra.Command, args []string) error {
			opt.mode = strings.ToLower(opt.mode)
			switch opt.mode {
			case "schema_stats":
				return runExportSchemaStats(&opt)
			case "stmt_summary":
				return runExportStmtSummary(&opt)
			default:
				return fmt.Errorf("unknonw export mode %v", opt.mode)
			}

		},
	}
	cmd.Flags().StringVar(&opt.mode, "mode", "", "schema_stats: export schema and stats from TiDB; stmt_summary: export queries from the statement_summary table (schema_stats / stmt_summary)")
	cmd.Flags().StringVar(&opt.db.addr, "addr", "127.0.0.1", "address of the target TiDB")
	cmd.Flags().StringVar(&opt.db.port, "port", "4000", "port of the target TiDB")
	cmd.Flags().StringVar(&opt.db.statusPort, "status-port", "10080", "status port of the target TiDB")
	cmd.Flags().StringVar(&opt.db.user, "user", "", "user name to access the target TiDB")
	cmd.Flags().StringVar(&opt.db.password, "password", "", "password to access the target TiDB")
	cmd.Flags().StringVar(&opt.dir, "schema-stats-dir", "", "destination directory to store exported schemas and statistics (only for schema_stats mode)")
	cmd.Flags().StringSliceVar(&opt.tables, "tables", nil, "tables to export, if nil export all tables' schema and stats (only for schema_stats mode)")
	cmd.Flags().StringVar(&opt.queryFile, "query-file", "", "file path to store queries (only for stmt_summary mode)")
	return cmd
}

func runExportStmtSummary(opt *exportOpt) error {
	db, err := connectDB(opt.db, "information_schema")
	if err != nil {
		return fmt.Errorf("connect to DB error: %v", err)
	}
	ver, err := db.getVersion()
	if err != nil {
		return fmt.Errorf("get DB version error: %v", err)
	}
	if compareVer(ver, "4.0") == -1 {
		return fmt.Errorf("TiDB:%v doesn't support statement summary", opt.db.version)
	}
	opt.queryFile = strings.TrimSpace(opt.queryFile)
	if opt.queryFile == "" {
		return fmt.Errorf("no file path to store queries")
	}

	return exportQueriesFromStmtSummary(db, opt.queryFile)
}

func exportQueriesFromStmtSummary(db *tidbHandler, dstFile string) error {
	rows, err := db.db.Query("SELECT QUERY_SAMPLE_TEXT FROM information_schema.cluster_statements_summary_history WHERE lower(QUERY_SAMPLE_TEXT) LIKE '%select%'")
	if err != nil {
		return fmt.Errorf("select queries from information_schema.cluster_statements_summary_history error: %v", err)
	}
	defer rows.Close()
	var queries []string
	for rows.Next() {
		var query string
		if err := rows.Scan(&query); err != nil {
			return fmt.Errorf("scan result error: %v", err)
		}
		queries = append(queries, query)
	}

	file, err := os.OpenFile(dstFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0666)
	if err != nil {
		return err
	}
	defer file.Close()
	buf := bufio.NewWriter(file)
	for _, q := range queries {
		if _, err := buf.Write([]byte(q + ";\n")); err != nil {
			return err
		}
	}
	if err := buf.Flush(); err != nil {
		return err
	}
	fmt.Printf("export queries from statement_summary into %v successfully\n", dstFile)
	return nil
}

func runExportSchemaStats(opt *exportOpt) error {
	if opt.dir == "" {
		return fmt.Errorf("please specific a destination directory")
	}
	if err := os.MkdirAll(opt.dir, 0776); err != nil {
		return fmt.Errorf("create destination directory error: %v", err)
	}
	db, err := connectDB(opt.db, "mysql")
	if err != nil {
		return fmt.Errorf("connect to DB error: %v", err)
	}
	return exportSchemaStats(db, opt.dir, opt.tables)
}

func exportSchemaStats(db *tidbHandler, dir string, tables []string) error {
	dbs, err := db.getDBs()
	if err != nil {
		return fmt.Errorf("get databases error: %v", err)
	}
	tablesMap := stringSliceToMap(tables)
	for _, dbName := range dbs {
		tables, err := db.getTables(dbName)
		if err != nil {
			return fmt.Errorf("get tables from DB: %v, error: %v", dbName, err)
		}
		for _, tableName := range tables {
			if len(tablesMap) > 0 {
				if _, ok := tablesMap[tableName]; !ok {
					continue
				}
			}
			if err := exportTableSchemas(db, dbName, tableName, dir); err != nil {
				return fmt.Errorf("export table: %v schema error: %v", tableName, err)
			}
			if err := exportTableStats(db, dbName, tableName, dir); err != nil {
				return fmt.Errorf("export table: %v stats error: %v", tableName, err)
			}
		}
	}
	return nil
}

func exportTableSchemas(db *tidbHandler, dbName, table, dir string) error {
	showSQL := fmt.Sprintf("show create table `%v`.`%v`", dbName, table)
	rows, err := db.db.Query(showSQL)
	if err != nil {
		return fmt.Errorf("exec SQL: %v error: %v", showSQL, err)
	}
	defer rows.Close()
	rows.Next()
	var createSQL string
	if err := rows.Scan(&table, &createSQL); err != nil {
		return fmt.Errorf("scan rows error: %v", err)
	}

	fpath := schemaPath(dbName, table, dir)
	err = ioutil.WriteFile(fpath, []byte(createSQL), 0666)
	fmt.Printf("export schema of %v.%v into %v\n", dbName, table, fpath)
	return err
}

func exportTableStats(db *tidbHandler, dbName, table, dir string) error {
	addr := fmt.Sprintf("http://%v:%v/stats/dump/%v/%v", db.opt.addr, db.opt.statusPort, dbName, table)
	resp, err := http.Get(addr)
	if err != nil {
		return fmt.Errorf("request URL: %v error: %v", addr, err)
	}
	stats, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read data from URL: %v response error: %v", addr, err)
	}
	if resp.StatusCode != 200 {
		return fmt.Errorf("request URL: %v server error: %v", addr, string(stats))
	}
	fpath := statsPath(dbName, table, dir)
	fmt.Printf("export stats of %v.%v into %v\n", dbName, table, fpath)
	return ioutil.WriteFile(fpath, stats, 0666)
}
