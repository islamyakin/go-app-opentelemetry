package config

import (
	"github.com/islamyakin/go-app-opentelemtry/model"
	"github.com/uptrace/opentelemetry-go-extra/otelgorm"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"log"
	"strconv"
	"time"
)

func InitDB() *gorm.DB {
	dsn := "host=" + Db_host + " user=postgres password=bukaevent dbname=bukaevent port=5432 sslmode=disable TimeZone=Asia/Jakarta"
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})

	if err != nil {
		log.Panicf("can't connect to db: %s", err)
	}
	if err := db.Use(otelgorm.NewPlugin()); err != nil {
		log.Panicf("error when using tracing otel gorm: %s", err)
	}
	sqlDb, _ := db.DB()

	mConn, err := strconv.Atoi(Db_max_conn)
	if err != nil {
		log.Panicf("error when convert DB_Max_con to integer")
	}
	sqlDb.SetMaxOpenConns(mConn) //harus e sih 100 ya
	sqlDb.SetMaxIdleConns(10)
	sqlDb.SetConnMaxLifetime(30 * time.Minute)

	//migrasi
	if err := db.AutoMigrate(&model.Event{}); err != nil {
		log.Panicf("Migrasi event gagal: %v", err)
	}

	var data model.Event
	tx := db.First(&data, 1)
	if tx.Error != nil {
		if tx.Error.Error() == "record not found" {

			log.Print("record not found")
			dataInsert := model.Event{
				Title: "Konser SOD VOl5",
				Desc:  "Konser Sounds Of Downton yang ke 5 kalinya",
				Quota: 1000000,
				Price: 800000,
			}
			if result := db.Create(&dataInsert); result.Error != nil {
				log.Panicf("Insert example gagal: %v", err)
			}
		} else {
			log.Panicf("error ketika get data id 1: %v", tx.Error)
		}
	}
	return db
}
