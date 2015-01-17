package main

import (
	"log"
	"net/http"

	"github.com/auth0/go-jwt-middleware"
	"github.com/codegangsta/negroni"
	"github.com/dgrijalva/jwt-go"
	"github.com/gorilla/mux"
	"github.com/jmcvetta/randutil"
	"github.com/nlf/boltons"
	"github.com/tomsteele/shellsquid/app"
	"github.com/tomsteele/shellsquid/config"
	"github.com/tomsteele/shellsquid/handlers"
	"github.com/tomsteele/shellsquid/middleware"
	"github.com/tomsteele/shellsquid/models"
	"github.com/unrolled/render"
)

func main() {
	conf, err := config.New("./conf.json")
	if err != nil {
		log.Fatalf("Error parsing confuration file: %s", err.Error())
	}
	db, err := boltons.Open(conf.BoltDBFile, 0600, nil)
	if err != nil {
		log.Fatal("Error opening db: %s", err.Error())
	}
	defer db.Close()

	keys, err := db.Keys(models.User{})
	if err != nil {
		log.Fatal("Error getting keys from db: %s", err.Error())
	}
	if len(keys) == 0 {
		log.Println("No users found creating admin@localhost user with random password")
		random, err := randutil.AlphaString(10)
		if err != nil {
			log.Fatalf("Error generating password: %s", err.Error())
		}
		firstUser, err := models.NewUser("admin@localhost", []byte(random))
		if err != nil {
		}
		if err := db.Save(firstUser); err != nil {
			log.Fatalf("Error saving first user to db: %s", err.Error())
		}
		log.Printf("admin@localhost password set to %s", random)
	}

	serverApp := &app.App{
		DB:        db,
		JWTSecret: []byte(conf.JWTKey),
		Render:    render.New(),
	}

	if conf.Proxy.SSL.Enabled {
		sslMux := http.NewServeMux()
		sslMux.HandleFunc("/", handlers.Proxy(serverApp, false))
		sslRecovery := negroni.NewRecovery()
		sslRecovery.PrintStack = false
		sslProxy := negroni.New(sslRecovery)
		sslProxy.UseHandler(sslMux)
		go func() {
			log.Fatal(http.ListenAndServeTLS(conf.Proxy.SSL.Listener, conf.Proxy.SSL.Cert, conf.Proxy.SSL.Key, sslProxy))
		}()
	}

	if conf.Proxy.HTTP.Enabled {
		httpMux := http.NewServeMux()
		httpMux.HandleFunc("/", handlers.Proxy(serverApp, false))
		httpRecovery := negroni.NewRecovery()
		httpRecovery.PrintStack = false
		httpProxy := negroni.New(httpRecovery)
		httpProxy.UseHandler(httpMux)
		go func() {
			log.Fatal(http.ListenAndServe(conf.Proxy.HTTP.Listener, httpProxy))
		}()
	}

	jwtMiddleware := jwtmiddleware.New(jwtmiddleware.Options{
		ValidationKeyGetter: func(token *jwt.Token) (interface{}, error) {
			return serverApp.JWTSecret, nil
		},
	})

	r := mux.NewRouter()
	api := mux.NewRouter()
	r.HandleFunc("/api/token", handlers.UserToken(serverApp)).Methods("POST")

	api.HandleFunc("/api/users", handlers.CreateUser(serverApp)).Methods("POST")
	api.HandleFunc("/api/users", handlers.IndexUser(serverApp)).Methods("GET")
	api.HandleFunc("/api/users/{id}", handlers.ShowUser(serverApp)).Methods("GET")
	api.HandleFunc("/api/users/{id}", handlers.DeleteUser(serverApp)).Methods("DELETE")
	api.HandleFunc("/api/users/{id}", handlers.UpdateUser(serverApp)).Methods("PUT")
	api.HandleFunc("/api/records", handlers.CreateRecord(serverApp)).Methods("POST")
	api.HandleFunc("/api/records", handlers.IndexRecord(serverApp)).Methods("GET")
	api.HandleFunc("/api/records/{id}", handlers.ShowRecord(serverApp)).Methods("GET")
	api.HandleFunc("/api/records/{id}", handlers.DeleteRecord(serverApp)).Methods("DELETE")
	api.HandleFunc("/api/records/{id}", handlers.UpdateRecord(serverApp)).Methods("PUT")

	r.PathPrefix("/api/records").PathPrefix("/api/users").Handler(negroni.New(
		negroni.HandlerFunc(jwtMiddleware.HandlerWithNext),
		negroni.HandlerFunc(middleware.SetUserContext(serverApp)),
		negroni.Wrap(api),
	))

	server := negroni.New(
		negroni.NewLogger(),
		negroni.NewStatic(http.Dir("client/dist")),
		negroni.NewRecovery(),
	)

	server.UseHandler(r)
	log.Fatal(http.ListenAndServeTLS(conf.Admin.Listener, conf.Admin.Cert, conf.Admin.Key, server))
}