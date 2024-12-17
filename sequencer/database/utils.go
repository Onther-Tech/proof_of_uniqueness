package database

import (
	"context"
	"database/sql"
	"fmt"
	"math/big"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"
	"tokamak-sybil-resistance/common"
	"tokamak-sybil-resistance/log"

	"github.com/gobuffalo/packr/v2"
	"github.com/jmoiron/sqlx"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
	migrate "github.com/rubenv/sql-migrate"
	"github.com/russross/meddler"
	"golang.org/x/sync/semaphore"
)

var migrations *migrate.PackrMigrationSource

func init() {
	migrations = &migrate.PackrMigrationSource{
		Box: packr.New("hermez-db-migrations", "./migrations"),
	}
	ms, err := migrations.FindMigrations()
	if err != nil {
		panic(err)
	}
	if len(ms) == 0 {
		panic(fmt.Errorf("no SQL migrations found"))
	}
}

// MigrationsUp runs the SQL migrations Up
func MigrationsUp(db *sql.DB) error {
	nMigrations, err := migrate.Exec(db, "postgres", migrations, migrate.Up)
	if err != nil {
		return common.Wrap(err)
	}
	log.Info("successfully ran ", nMigrations, " migrations Up")
	return nil
}

// MigrationsDown runs the SQL migrations Down,
// migrationsToRun specifies how many migrations will be run, 0 means any.
func MigrationsDown(db *sql.DB, migrationsToRun uint) error {
	nMigrations, err := migrate.ExecMax(db, "postgres", migrations, migrate.Down, int(migrationsToRun))
	if err != nil {
		return common.Wrap(err)
	}
	if migrationsToRun != 0 && nMigrations != int(migrationsToRun) {
		return common.Wrap(
			fmt.Errorf("Unexpected amount of migrations applied. Expected = %d, actual = %d", migrationsToRun, nMigrations),
		)
	}
	log.Info("successfully ran ", nMigrations, " migrations Down")
	return nil
}

// ConnectSQLDB connects to the SQL DB
func ConnectSQLDB(port int, host, user, password, name string) (*sqlx.DB, error) {
	// Init meddler
	initMeddler()
	meddler.Default = meddler.PostgreSQL
	// Stablish connection
	psqlconn := fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=disable",
		host,
		port,
		user,
		password,
		name,
	)
	db, err := sqlx.Connect("postgres", psqlconn)
	if err != nil {
		return nil, common.Wrap(err)
	}
	return db, nil
}

// InitSQLDB runs migrations and registers meddlers
func InitSQLDB(port int, host, user, password, name string) (*sqlx.DB, error) {
	db, err := ConnectSQLDB(port, host, user, password, name)
	if err != nil {
		return nil, common.Wrap(err)
	}
	// Run DB migrations
	if err := MigrationsUp(db.DB); err != nil {
		return nil, common.Wrap(err)
	}
	return db, nil
}

// InitTestSQLDB opens test PostgreSQL database
func InitTestSQLDB() (*sqlx.DB, error) {
	err := godotenv.Load("../.env")
	if err != nil {
		log.Info("Error loading .env file")
	}
	host := os.Getenv("PGHOST")
	if host == "" {
		host = "localhost"
	}
	port, _ := strconv.Atoi(os.Getenv("PGPORT"))
	if port == 0 {
		port = 5432
	}
	user := os.Getenv("PGUSER")
	if user == "" {
		user = "hermez"
	}
	pass := os.Getenv("PGPASSWORD")
	if pass == "" {
		panic("No PGPASSWORD envvar specified")
	}
	dbname := os.Getenv("PGDATABASE")
	if dbname == "" {
		dbname = "tokamak"
	}
	return InitSQLDB(port, host, user, pass, dbname)
}

// APIConnectionController is used to limit the SQL open connections used by the API
type APIConnectionController struct {
	smphr   *semaphore.Weighted
	timeout time.Duration
}

// NewAPIConnectionController initialize APIConnectionController
func NewAPIConnectionController(maxConnections int, timeout time.Duration) *APIConnectionController {
	return &APIConnectionController{
		smphr:   semaphore.NewWeighted(int64(maxConnections)),
		timeout: timeout,
	}
}

// Acquire reserves a SQL connection. If the connection is not acquired
// within the timeout, the function will return an error
func (acc *APIConnectionController) Acquire() (context.CancelFunc, error) {
	ctx, cancel := context.WithTimeout(context.Background(), acc.timeout) //nolint:govet
	return cancel, acc.smphr.Acquire(ctx, 1)
}

// Release frees a SQL connection
func (acc *APIConnectionController) Release() {
	acc.smphr.Release(1)
}

// Rollback an sql transaction, and log the error if it's not nil
func Rollback(txn *sqlx.Tx) {
	if err := txn.Rollback(); err != nil {
		log.Errorw("Rollback", "err", err)
	}
}

// BigIntMeddler encodes or decodes the field value to or from JSON
type BigIntMeddler struct{}

// PreRead is called before a Scan operation for fields that have the BigIntMeddler
func (b BigIntMeddler) PreRead(fieldAddr interface{}) (scanTarget interface{}, err error) {
	// give a pointer to a byte buffer to grab the raw data
	return new(string), nil
}

// PostRead is called after a Scan operation for fields that have the BigIntMeddler
func (b BigIntMeddler) PostRead(fieldPtr, scanTarget interface{}) error {
	ptr := scanTarget.(*string)
	if ptr == nil {
		return common.Wrap(fmt.Errorf("BigIntMeddler.PostRead: nil pointer"))
	}
	field := fieldPtr.(**big.Int)
	var ok bool
	*field, ok = new(big.Int).SetString(*ptr, 10)
	if !ok {
		return common.Wrap(fmt.Errorf("big.Int.SetString failed on \"%v\"", *ptr))
	}
	return nil
}

// PreWrite is called before an Insert or Update operation for fields that have the BigIntMeddler
func (b BigIntMeddler) PreWrite(fieldPtr interface{}) (saveValue interface{}, err error) {
	field := fieldPtr.(*big.Int)

	return field.String(), nil
}

// BigIntNullMeddler encodes or decodes the field value to or from JSON
type BigIntNullMeddler struct{}

// PreRead is called before a Scan operation for fields that have the BigIntNullMeddler
func (b BigIntNullMeddler) PreRead(fieldAddr interface{}) (scanTarget interface{}, err error) {
	return &fieldAddr, nil
}

// PostRead is called after a Scan operation for fields that have the BigIntNullMeddler
func (b BigIntNullMeddler) PostRead(fieldPtr, scanTarget interface{}) error {
	field := fieldPtr.(**big.Int)
	ptrPtr := scanTarget.(*interface{})
	if *ptrPtr == nil {
		// null column, so set target to be zero value
		*field = nil
		return nil
	}
	// not null
	ptr := (*ptrPtr).([]byte)
	if ptr == nil {
		return common.Wrap(fmt.Errorf("BigIntMeddler.PostRead: nil pointer"))
	}
	var ok bool
	*field, ok = new(big.Int).SetString(string(ptr), 10)
	if !ok {
		return common.Wrap(fmt.Errorf("big.Int.SetString failed on \"%v\"", string(ptr)))
	}

	return nil
}

// PreWrite is called before an Insert or Update operation for fields that have the BigIntNullMeddler
func (b BigIntNullMeddler) PreWrite(fieldPtr interface{}) (saveValue interface{}, err error) {
	field := fieldPtr.(*big.Int)
	if field == nil {
		return nil, nil
	}
	return field.String(), nil
}

// initMeddler registers tags to be used to read/write from SQL DBs using meddler
func initMeddler() {
	meddler.Register("bigint", BigIntMeddler{})
	meddler.Register("bigintnull", BigIntNullMeddler{})
}

// BulkInsert performs a bulk insert with a single statement into the specified table.  Example:
// `db.BulkInsert(myDB, "INSERT INTO block (eth_block_num, timestamp, hash) VALUES %s", blocks[:])`
// Note that all the columns must be specified in the query, and they must be
// in the same order as in the table.
// Note that the fields in the structs need to be defined in the same order as
// in the table columns.
func BulkInsert(db meddler.DB, q string, args interface{}) error {
	arrayValue := reflect.ValueOf(args)
	arrayLen := arrayValue.Len()
	valueStrings := make([]string, 0, arrayLen)
	var arglist = make([]interface{}, 0)
	for i := 0; i < arrayLen; i++ {
		arg := arrayValue.Index(i).Addr().Interface()
		elemArglist, err := meddler.Default.Values(arg, true)
		if err != nil {
			return common.Wrap(err)
		}
		arglist = append(arglist, elemArglist...)
		value := "("
		for j := 0; j < len(elemArglist); j++ {
			value += fmt.Sprintf("$%d, ", i*len(elemArglist)+j+1)
		}
		value = value[:len(value)-2] + ")"
		valueStrings = append(valueStrings, value)
	}
	stmt := fmt.Sprintf(q, strings.Join(valueStrings, ","))
	_, err := db.Exec(stmt, arglist...)
	return common.Wrap(err)
}

// SlicePtrsToSlice converts any []*Foo to []Foo
func SlicePtrsToSlice(slice interface{}) interface{} {
	v := reflect.ValueOf(slice)
	vLen := v.Len()
	typ := v.Type().Elem().Elem()
	res := reflect.MakeSlice(reflect.SliceOf(typ), vLen, vLen)
	for i := 0; i < vLen; i++ {
		res.Index(i).Set(v.Index(i).Elem())
	}
	return res.Interface()
}
