/*
 * Copyright (c) 2021 yedf. All rights reserved.
 * Use of this source code is governed by a BSD-style
 * license that can be found in the LICENSE file.
 */

package examples

import (
	"database/sql"

	"github.com/gin-gonic/gin"
	"github.com/yedf/dtm/common"
	"github.com/yedf/dtm/dtmcli"
	"github.com/yedf/dtm/dtmcli/dtmimp"
)

func init() {
	setupFuncs["SagaGormBarrierSetup"] = func(app *gin.Engine) {
		app.POST(BusiAPI+"/SagaBTransOutGorm", common.WrapHandler(sagaGormBarrierTransOut))
	}
	addSample("saga_gorm_barrier", func() string {
		dtmimp.Logf("a busi transaction begin")
		req := &TransReq{Amount: 30}
		saga := dtmcli.NewSaga(DtmHttpServer, dtmcli.MustGenGid(DtmHttpServer)).
			Add(Busi+"/SagaBTransOutGorm", Busi+"/SagaBTransOutCompensate", req).
			Add(Busi+"/SagaBTransIn", Busi+"/SagaBTransInCompensate", req)
		dtmimp.Logf("busi trans submit")
		err := saga.Submit()
		dtmimp.FatalIfError(err)
		return saga.Gid
	})

}

func sagaGormBarrierTransOut(c *gin.Context) (interface{}, error) {
	req := reqFrom(c)
	barrier := MustBarrierFromGin(c)
	tx := dbGet().DB.Begin()
	return dtmcli.MapSuccess, barrier.Call(tx.Statement.ConnPool.(*sql.Tx), func(tx1 *sql.Tx) error {
		return tx.Exec("update dtm_busi.user_account set balance = balance + ? where user_id = ?", -req.Amount, 2).Error
	})
}
