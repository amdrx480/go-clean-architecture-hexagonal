package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	_driverFactory "github.com/amdrx480/go-clean-architecture-hexagonal/drivers"
	"github.com/amdrx480/go-clean-architecture-hexagonal/utils"

	_userUseCase "github.com/amdrx480/go-clean-architecture-hexagonal/businesses/users"
	_userController "github.com/amdrx480/go-clean-architecture-hexagonal/controllers/users"

	_dbDriver "github.com/amdrx480/go-clean-architecture-hexagonal/drivers/mysql"

	_middleware "github.com/amdrx480/go-clean-architecture-hexagonal/app/middlewares"
	_routes "github.com/amdrx480/go-clean-architecture-hexagonal/app/routes"

	echo "github.com/labstack/echo/v4"
)

type operation func(ctx context.Context) error

func main() {
	configDB := _dbDriver.DBConfig{
		DB_USERNAME: utils.GetConfig("DB_USERNAME"),
		DB_PASSWORD: utils.GetConfig("DB_PASSWORD"),
		DB_HOST:     utils.GetConfig("DB_HOST"),
		DB_PORT:     utils.GetConfig("DB_PORT"),
		DB_NAME:     utils.GetConfig("DB_NAME"),
	}

	db := configDB.InitDB()

	_dbDriver.MigrateDB(db)

	configJWT := _middleware.JWTConfig{
		SecretKey:       utils.GetConfig("JWT_SECRET_KEY"),
		ExpiresDuration: 1,
	}

	configLogger := _middleware.LoggerConfig{
		Format: "[${time_rfc3339}] ${status} ${method} ${host} ${path} ${latency_human}" + "\n",
	}

	e := echo.New()

	userRepo := _driverFactory.NewUserRepository(db)
	userUsecase := _userUseCase.NewUserUseCase(userRepo, &configJWT)
	userCtrl := _userController.NewAuthController(userUsecase)

	routesInit := _routes.ControllerList{
		LoggerMiddleware: configLogger.Init(),
		JWTMiddleware:    configJWT.Init(),
		UserController:   *userCtrl,
	}

	routesInit.RegisterRoutes(e)

	go func() {
		if err := e.Start(":1323"); err != nil && err != http.ErrServerClosed {
			e.Logger.Fatal("shutting down the server")
		}
	}()

	wait := gracefulShutdown(context.Background(), 2*time.Second, map[string]operation{
		"database": func(ctx context.Context) error {
			return _dbDriver.CloseDB(db)
		},
		"http-server": func(ctx context.Context) error {
			return e.Shutdown(context.Background())
		},
	})

	<-wait
}

// gracefulShutdown performs application shut down gracefully.
func gracefulShutdown(ctx context.Context, timeout time.Duration, ops map[string]operation) <-chan struct{} {
	wait := make(chan struct{})
	go func() {
		s := make(chan os.Signal, 1)

		// add any other syscalls that you want to be notified with
		signal.Notify(s, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
		<-s

		log.Println("shutting down")

		// set timeout for the ops to be done to prevent system hang
		timeoutFunc := time.AfterFunc(timeout, func() {
			log.Printf("timeout %d ms has been elapsed, force exit", timeout.Milliseconds())
			os.Exit(0)
		})

		defer timeoutFunc.Stop()

		var wg sync.WaitGroup

		// Do the operations asynchronously to save time
		for key, op := range ops {
			wg.Add(1)
			innerOp := op
			innerKey := key
			go func() {
				defer wg.Done()

				log.Printf("cleaning up: %s", innerKey)
				if err := innerOp(ctx); err != nil {
					log.Printf("%s: clean up failed: %s", innerKey, err.Error())
					return
				}

				log.Printf("%s was shutdown gracefully", innerKey)
			}()
		}

		wg.Wait()

		close(wait)
	}()

	return wait
}
