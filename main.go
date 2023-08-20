package main

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/islamyakin/go-app-opentelemtry/config"
	"github.com/islamyakin/go-app-opentelemtry/config/helper"
	"github.com/islamyakin/go-app-opentelemtry/config/tracing"
	"github.com/islamyakin/go-app-opentelemtry/model"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"gorm.io/gorm/clause"
)

func init() {
	eDbHost, exist := os.LookupEnv("DB_HOST")
	if exist {
		config.Db_host = eDbHost
	}

	ePort, exist := os.LookupEnv("PORT")
	if exist {
		config.Port = ePort
	}

	eOtelHost, exist := os.LookupEnv("OTEL_HOST")
	if exist {
		config.Otel_host = eOtelHost
	}

	eDbMaxConn, exist := os.LookupEnv("DB_MAX_CONN")
	if exist {
		config.Db_max_conn = eDbMaxConn
	}

	ePaymentHost, exist := os.LookupEnv("PAYMENT_HOST")
	if exist {
		config.Payment_host = ePaymentHost
	}

	eSampler, exist := os.LookupEnv("OTEL_SAMPLER_RATIO")
	if exist {
		e_sampler_float, err := strconv.ParseFloat(eSampler, 64)
		if err != nil {
			log.Panic(err)
		}

		config.Sampler = e_sampler_float
	}
}

func main() {

	// database
	db := config.InitDB()

	// ctx, cancel := context.WithCancel(context.Background())
	ctx := context.Background()

	// Trace Provider
	tp, err := tracing.InitTraceProvider()
	if err != nil {
		log.Fatal(err)
	}

	defer func() {
		// Do not make the application hang when it is tp.
		ctx, cancel := context.WithTimeout(ctx, time.Second*5)
		defer cancel()
		if err := tp.Shutdown(ctx); err != nil {
			log.Fatal(err)
		}
	}()

	// Metric Provider
	mp, err := tracing.InitMeterProvider(ctx)
	if err != nil {
		log.Fatal(err)
	}

	defer func() {
		ctx, cancel := context.WithTimeout(ctx, time.Second*5)
		defer cancel()
		if err := mp.Shutdown(ctx); err != nil {
			log.Fatal(err)
		}
	}()

	meter := mp.Meter(config.Name)

	// Create conter metric
	apiCounter, err := meter.Int64Counter("api counter")
	if err != nil {
		log.Fatalf("can't initialize counter api hit: %v", err)
	}

	// Gin
	r := gin.Default()
	r.Use(otelgin.Middleware(config.Name)) // middleware otelgin

	eventGroup := r.Group("/event")
	eventGroup.GET("/:id", func(c *gin.Context) {

		var data model.Event
		id := c.Param("id")

		ctx, span := tp.Tracer(config.Name).Start(c.Request.Context(), "Query to DB")
		defer span.End()

		d := db.WithContext(ctx).First(&data, id)

		if d.Error != nil {
			span.SetStatus(codes.Error, "error get query")
			span.RecordError(d.Error)
			c.JSON(http.StatusInternalServerError, "error get query")
			return
		}

		span.AddEvent("request finish")

		c.JSON(http.StatusOK, gin.H{
			"data": data,
		})
	})

	eventGroup.POST("/:id/buy", func(c *gin.Context) {

		id := c.Param("id")
		var dataGet model.Event

		dbTx := db.Begin()

		// check remaning quota
		ctxQuota, spanQuota := tp.Tracer(config.Name).Start(c.Request.Context(), "check remaning quota")
		defer spanQuota.End()

		tx := dbTx.Clauses(clause.Locking{Strength: "UPDATE"}).WithContext(ctxQuota).First(&dataGet, id) // locking
		if tx.Error != nil {
			dbTx.Rollback()
			spanQuota.RecordError(tx.Error)
			spanQuota.SetStatus(codes.Error, "error when get data for check remaining quota")
			c.JSON(http.StatusInternalServerError, tx.Error.Error())
			return
		}

		// sold out
		if dataGet.Quota <= 0 {
			dbTx.Rollback()
			c.JSON(http.StatusConflict, "tiket sold out")
			return
		}

		// if ticket still available
		ctxBuy, spanBuy := tp.Tracer(config.Name).Start(ctxQuota, "buy a ticket")
		defer spanBuy.End()

		finalQuota := dataGet.Quota - 1 // decrease 1

		tx = dbTx.WithContext(ctxBuy).Model(&dataGet).Update("quota", finalQuota)
		if tx.Error != nil {
			dbTx.Rollback()
			spanBuy.RecordError(tx.Error)
			spanBuy.SetStatus(codes.Error, "error update ticket data")
			c.JSON(http.StatusInternalServerError, tx.Error.Error())
			return
		}

		// success
		dbTx.Commit()
		apiCounter.Add(c.Request.Context(), 1, metric.WithAttributes(
			attribute.String("method", c.Request.Method),
			attribute.String("endpoint", c.FullPath()),
			attribute.String("status", "success"),
		)) // increase meter
		c.JSON(http.StatusOK, "ok tiket berhasil dibeli")
	})

	v2 := r.Group("/v2")
	eventV2 := v2.Group("/event")

	eventV2.POST("/:id/buy", func(c *gin.Context) {
		id := c.Param("id")

		ctx, span := tp.Tracer(config.Name).Start(c.Request.Context(), "Convert string to int for ID")
		defer span.End()

		userID, err := strconv.Atoi(id)
		if err != nil {
			span.SetStatus(codes.Error, "error ehen convert strin to int ID user")
			span.RecordError(err)
			c.JSON(http.StatusInternalServerError, err.Error())
			return
		}

		// check balance
		ctx, span = tp.Tracer(config.Name).Start(ctx, "check balance")
		defer span.End()

		var payload = model.PayloadRequestBalance{
			UserId: userID,
		}

		// setup Baggages
		baggageUserId, _ := baggage.NewMember("user_id", id)
		baggageMock, _ := baggage.NewMember("test_baggages", "test-value-baggae") // the value can't have space
		b, _ := baggage.New(baggageUserId, baggageMock)
		ctx = baggage.ContextWithBaggage(ctx, b)

		// request to payment service
		res, err := helper.HttpRequest(ctx, "POST", config.Payment_host+"/balance-check", payload)

		if err != nil {
			span.SetStatus(codes.Error, "error request balance check")
			span.RecordError(err)
			c.JSON(http.StatusInternalServerError, err.Error())
			return
		}

		// parse data
		ctx, span = tp.Tracer(config.Name).Start(ctx, "parse response data")
		defer span.End()

		var dataParsed model.PayloadResponseBalance
		if err := json.Unmarshal(res.Body, &dataParsed); err != nil {
			span.SetStatus(codes.Error, "error when paese response data")
			span.RecordError(err)
			c.JSON(http.StatusInternalServerError, err.Error())
			return
		}

		// check
		_, span = tp.Tracer(config.Name).Start(ctx, "balance reduction")
		defer span.End()

		if dataParsed.Balance < 100000 {
			msg := "balance is not enough"
			span.SetStatus(codes.Error, msg)
			span.RecordError(errors.New(msg))
			span.SetAttributes(attribute.Int64("balance", dataParsed.Balance))
			c.JSON(http.StatusInternalServerError, msg)
			return
		}

		c.JSON(http.StatusOK, "OK")
	})

	r.GET("/", func(ctx *gin.Context) {
		ctx.String(http.StatusOK, "Hola, ini order service")
	})

	srv := &http.Server{
		Addr:         ":" + config.Port,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
	}

	// run server
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %s\n", err)
		}
	}()

	// shutdown server
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutdown server ....")

	ctxServer, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctxServer); err != nil {
		log.Fatal("Shutdown server:", err)
	}

	log.Println("Server exiting")
}
