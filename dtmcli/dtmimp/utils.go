/*
 * Copyright (c) 2021 yedf. All rights reserved.
 * Use of this source code is governed by a BSD-style
 * license that can be found in the LICENSE file.
 */

package dtmimp

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"

	"github.com/go-resty/resty/v2"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// AsError wrap a panic value as an error
func AsError(x interface{}) error {
	LogRedf("panic wrapped to error: '%v'", x)
	if e, ok := x.(error); ok {
		return e
	}
	return fmt.Errorf("%v", x)
}

// P2E panic to error
func P2E(perr *error) {
	if x := recover(); x != nil {
		*perr = AsError(x)
	}
}

// E2P error to panic
func E2P(err error) {
	if err != nil {
		panic(err)
	}
}

// CatchP catch panic to error
func CatchP(f func()) (rerr error) {
	defer P2E(&rerr)
	f()
	return nil
}

// PanicIf name is clear
func PanicIf(cond bool, err error) {
	if cond {
		panic(err)
	}
}

// MustAtoi 走must逻辑
func MustAtoi(s string) int {
	r, err := strconv.Atoi(s)
	if err != nil {
		E2P(errors.New("convert to int error: " + s))
	}
	return r
}

// OrString return the first not empty string
func OrString(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

// If ternary operator
func If(condition bool, trueObj interface{}, falseObj interface{}) interface{} {
	if condition {
		return trueObj
	}
	return falseObj
}

// MustMarshal checked version for marshal
func MustMarshal(v interface{}) []byte {
	b, err := json.Marshal(v)
	E2P(err)
	return b
}

// MustMarshalString string version of MustMarshal
func MustMarshalString(v interface{}) string {
	return string(MustMarshal(v))
}

// MustUnmarshal checked version for unmarshal
func MustUnmarshal(b []byte, obj interface{}) {
	err := json.Unmarshal(b, obj)
	E2P(err)
}

// MustUnmarshalString string version of MustUnmarshal
func MustUnmarshalString(s string, obj interface{}) {
	MustUnmarshal([]byte(s), obj)
}

// MustRemarshal marshal and unmarshal, and check error
func MustRemarshal(from interface{}, to interface{}) {
	b, err := json.Marshal(from)
	E2P(err)
	err = json.Unmarshal(b, to)
	E2P(err)
}

var logger *zap.SugaredLogger = nil

func init() {
	InitLog()
}

func InitLog() {
	config := zap.NewProductionConfig()
	config.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	if os.Getenv("DTM_DEBUG") != "" {
		config.Encoding = "console"
		config.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	}
	p, err := config.Build()
	if err != nil {
		log.Fatal("create logger failed: ", err)
	}
	logger = p.Sugar()
}

// Logf 输出日志
func Logf(fmt string, args ...interface{}) {
	logger.Infof(fmt, args...)
}

// LogRedf 采用红色打印错误类信息
func LogRedf(fmt string, args ...interface{}) {
	logger.Errorf(fmt, args...)
}

// FatalExitFunc Fatal退出函数，测试时被替换
var FatalExitFunc = func() { os.Exit(1) }

// LogFatalf 采用红色打印错误类信息， 并退出
func LogFatalf(fmt string, args ...interface{}) {
	fmt += "\n" + string(debug.Stack())
	LogRedf(fmt, args...)
	FatalExitFunc()
}

// LogIfFatalf 采用红色打印错误类信息， 并退出
func LogIfFatalf(condition bool, fmt string, args ...interface{}) {
	if condition {
		LogFatalf(fmt, args...)
	}
}

// FatalIfError 采用红色打印错误类信息， 并退出
func FatalIfError(err error) {
	LogIfFatalf(err != nil, "Fatal error: %v", err)
}

// GetFuncName get current call func name
func GetFuncName() string {
	pc, _, _, _ := runtime.Caller(1)
	nm := runtime.FuncForPC(pc).Name()
	return nm[strings.LastIndex(nm, ".")+1:]
}

// MayReplaceLocalhost when run in docker compose, change localhost to host.docker.internal for accessing host network
func MayReplaceLocalhost(host string) string {
	if os.Getenv("IS_DOCKER") != "" {
		return strings.Replace(host, "localhost", "host.docker.internal", 1)
	}
	return host
}

var sqlDbs sync.Map

// PooledDB get pooled sql.DB
func PooledDB(conf map[string]string) (*sql.DB, error) {
	dsn := GetDsn(conf)
	db, ok := sqlDbs.Load(dsn)
	if !ok {
		db2, err := StandaloneDB(conf)
		if err != nil {
			return nil, err
		}
		db = db2
		sqlDbs.Store(dsn, db)
	}
	return db.(*sql.DB), nil
}

// StandaloneDB get a standalone db instance
func StandaloneDB(conf map[string]string) (*sql.DB, error) {
	dsn := GetDsn(conf)
	Logf("opening standalone %s: %s", conf["driver"], strings.Replace(dsn, conf["password"], "****", 1))
	return sql.Open(conf["driver"], dsn)
}

// DBExec use raw db to exec
func DBExec(db DB, sql string, values ...interface{}) (affected int64, rerr error) {
	if sql == "" {
		return 0, nil
	}
	sql = GetDBSpecial().GetPlaceHoldSQL(sql)
	r, rerr := db.Exec(sql, values...)
	if rerr == nil {
		affected, rerr = r.RowsAffected()
		Logf("affected: %d for %s %v", affected, sql, values)
	} else {
		LogRedf("exec error: %v for %s %v", rerr, sql, values)
	}
	return
}

// GetDsn get dsn from map config
func GetDsn(conf map[string]string) string {
	host := MayReplaceLocalhost(conf["host"])
	driver := conf["driver"]
	dsn := map[string]string{
		"mysql": fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=true&loc=Local",
			conf["user"], conf["password"], host, conf["port"], conf["database"]),
		"postgres": fmt.Sprintf("host=%s user=%s password=%s dbname='%s' port=%s sslmode=disable",
			host, conf["user"], conf["password"], conf["database"], conf["port"]),
	}[driver]
	PanicIf(dsn == "", fmt.Errorf("unknow driver: %s", driver))
	return dsn
}

// CheckResponse 检查Response，返回错误
func CheckResponse(resp *resty.Response, err error) error {
	if err == nil && resp != nil {
		if resp.IsError() {
			return errors.New(resp.String())
		} else if strings.Contains(resp.String(), ResultFailure) {
			return ErrFailure
		} else if strings.Contains(resp.String(), ResultOngoing) {
			return ErrOngoing
		}
	}
	return err
}

// CheckResult 检查Result，返回错误
func CheckResult(res interface{}, err error) error {
	if err != nil {
		return err
	}
	resp, ok := res.(*resty.Response)
	if ok {
		return CheckResponse(resp, err)
	}
	if res != nil {
		str := MustMarshalString(res)
		if strings.Contains(str, ResultFailure) {
			return ErrFailure
		} else if strings.Contains(str, ResultOngoing) {
			return ErrOngoing
		}
	}
	return err
}
