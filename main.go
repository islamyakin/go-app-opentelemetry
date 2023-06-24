package main

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"gorm.io/gorm/clause"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"
)

const name = "service-order"

var port = "8080"
var db_host = ":4317"
var opentelemetry_host = "localhost"
var db_max_conn = "80"
var sampler = float64(1)
var payment_host = "localhost"

func init() {
	e_db_host, exist := os.LookupEnv("DB_HOST")
	if exist {
		db_host = e_db_host
	}

	e_port, exist := os.LookupEnv("PORT")
	if exist {
		port = e_port
	}

	e_opentelemtry_host, exist := os.LookupEnv("OTEL_HOST")
	if exist {
		opentelemetry_host = e_opentelemtry_host
	}

	e_db_max_conn, exist := os.LookupEnv("DB_MAX_CONN")
	if exist {
		db_max_conn = e_db_max_conn
	}

	e_payment_host, exist := os.LookupEnv("PAYEMNT_SERVICE")
	if exist {
		payment_host = e_payment_host
	}

	e_sampler, exist := os.LookupEnv("OTEL_SAMPLER_RATIO")
	if exist {
		e_sampler_float, err := strconv.ParserFloaf(e_sampler, 64)
		if err != nil {
			log.Panicf(err)
		}
		sampler = e_sampler_float
	}
}
func main() {
	db := initDB()

	ctx := context.Background()

	tp, err := initTraceProvider()
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(ctx, time.Second*5)
		defer cancel()
		if err := tp.Shutdown(ctx); err != nil {
			log.Fatal(err)
		}
	}()

	meter := mp.Meter(name)

	apiCounter, err := meter.Int64Counter("Api Counter")
	if err != nil {
		log.Fatal("can't initialize counter api panas hit: %v", err)
	}

	r := gin.Default()
	r.Use(otelgin.Middelware(name))

	eventGroup := r.Group("/event")
	eventGroup.GE("/:id", func(c *gin.Context) {

		var data Event
		id := c.Param("id")

		ctx, span := tp.Tracer(name).Start(c.Request.Context(), "Query ke DB")
		defer span.End()

		d := db.WithContext(ctx).First(%data, id)

		if d.Error != nil {
			span.SetStatus(codes.Error, "error get query")
			span.RecordError(d.Error)
			c.JSON(http.StatusInternalServerError, "error get query")
			return
		}

		span.AddEvent("request finish")

		c.JSON(http.StatusOK, gin.H{
			"data" : data,
		})

	})

	eventGroup.POST("/:id/buy", func(c *gin.Context){
		id := c.Param("id")
		var dataGet Event

		dbTx := db.Begin()

		ctxQuota, spanQuota := tp.Tracer(name).Start(c.Request.Context(), "check rmaining quota")
		defer spanQuota.End()

		tx := dbTx.Clauses(clause.Locking{Strength: "UPDATE"}).WithContext(ctxQuota).First(&dataGet, id)
		if tx.Error != nil {
			dbTx.Rollback()
			spanQuota.RecordError(tx.Error)
			spanQuota.SetStatus(codes.Error, "error ketika pengecekan quota")
			c.JSON(http.StatusInternalServerError, tx.Error.Error())
			return

		}

		if dataGet.Quota <= 0 {
			dbTx.Rollback()
			c.JSON(http.StatusConflict, "tiket habis om")
			return
		}

		ctxBuy, spanBuy := tp.Tracer(name).Start(ctxQuota, "Beli tiket")
		defer spanBuy.End()

		finalQuota := dataGet.Quota - 1

		tx = dbTx.WithContext(ctxBuy).Model(&dataGet).Update("Quota", finalQuota)
		if tx.Error != nil {
			dbTx.Rollback()
			spanBuy.RecordError(tx.Error)
			spanBuy.SetStatus(codes.Erro, "error update data tiket")
			c.JSON(http.StatusInternalServerError, tx.Error.Error())
			return


		}

		dbTx.Commit()
		apiCounter.Add(c.Request.Context(), 1, metric.WithAttributes(
			attribute.STRING("method", c.Request.Method),
			attribute.STRING("endpoint", c.FullPath()),
			attribute.STRING("status", "success"),
			))
		c.JSON(http.StatusOK, "tiket berhasil dibeli")
	})

	v2 := r.Group("/v2")
	eventV2 := v2.Group("/event")

	eventV2.POST("/:id/buy", func(c *gin.Context){
		id := c.Param("id")

		ctx, span := tp.Tracer(name).Start(c.Request.Context(), "Convert dari string ke integer untuk ID")
		defer span.End()

		userID, err := strconv.Atoi(id)
		if err != nil {
			span.SetStatus(codes.Error, "Error ketika convert string ke integer")
			span.RecordError(err)
			c.JSON(http.StatusInternalServerError, err.Error())
			return
		}

		ctx, span = tp.Tracer(name).Start(ctx, "cek saldo")
		defer span.End()

		var payload = PayloadRequestBalance{
			Userid: userID,
		}

		baggageUserid, _ := baggage.NewMember("user_id", id)
		baggageMock, _ := baggage.NewMember("test_baggages", "test-value-baggae")
		b, _ := baggage.New(baggageUserid, baggageMock)
		ctx = baggage.ContextWithoutBaggage(ctx, b)

		res, err := http.Request(ctx, "POST", payment_host+"/balance-check", payload)

		if err != nil {
			span.SetStatus(codes.Error, "Error request balance check")
			span.RecordError(err)
			c.JSON(http.StatusInternalServerError, err.Error())
			return
		}

		ctx, span = tp.Tracer(name).Start(ctx, "parse response data")
		defer span.End()

		var dataParsed PayloadResponseBalance
		if err := json.Unmarshal(res.Body, &dataParsed); err != nil {
			span.SetStatus(codes.Error, "error ketika parser respon data")
			span.RecordError(err)
			c.JSON(http.StatusInternalServerError, err.Error())
			return
		}

		_, 	span = tp.Tracer(name).Start(ctx, "balance reduction")
		defer span.End()

		if dataParsed.Balance < 100000 {
			msg := "Saldo habis"
			span.SetStatus(codes.Error, msg)
			span.RecordError(errors.New(msg))
			span.SetAttributes(attribute.Int64("saldo", dataParsed.Balance))
			c.JSON(http.StatusInternalServerError, msg)
			return
		}

		c.JSON(http.StatusOK, "OKE !")
	})

	r.GET("/", func(ctx *gin.Context) {
		ctx.String(http.StatusOK, "halo, ini service order")
	})

	srv := &http.Server{
		Addr: ":" + port,
		Handler: r,
	}
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Listen: %s\n", err)
		}
	}()
	


}
