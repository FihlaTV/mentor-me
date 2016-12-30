package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"html"
	"log"
	"net/http"
	"os"
	"regexp"
	"strconv"

	"github.com/lib/pq"

	"github.com/TheBeege/mentor-me/models"

	"github.com/gorilla/mux"
)

var (
	dbCon *sql.DB
)

type errorResponse struct {
	ErrorCode    int    `json:"error_code"`
	ErrorMessage string `json:"error_message"`
}

func main() {
	log.Println("It's go time!")
	dbHost := os.Getenv("dbhost")
	dbUser := os.Getenv("dbuser")
	dbPass := os.Getenv("dbpass")
	dbDatabase := os.Getenv("dbdatabase")
	conString := fmt.Sprintf("host=%s user=%s password=%s dbname=%s sslmode=disable",
		dbHost, dbUser, dbPass, dbDatabase)

	var err error
	dbCon, err = sql.Open("postgres", conString)
	if err != nil {
		log.Println("Error connecting to database:", err)
		return
	}
	defer dbCon.Close()

	router := mux.NewRouter().StrictSlash(true)
	//router.PathPrefix("/").Handler(http.StripPrefix("/", http.FileServer(http.Dir("./client/"))))
	router.HandleFunc("/", Index).Methods("GET")

	router.HandleFunc("/api/v1/user/{id:[0-9]+}", GetUser).Methods("GET")
	router.HandleFunc("/api/v1/user", NewUser).Methods("POST")

	router.HandleFunc("/api/v1/topic/{id:[0-9]+}", GetTopic).Methods("GET")
	router.HandleFunc("/api/v1/topic", NewTopic).Methods("POST")
	router.HandleFunc("/api/v1/topic_like/{part:.+}", GetTopicsLike).Methods("GET")

	router.HandleFunc("/api/v1/mentor_topic", NewMentorTopic).Methods("POST")
	router.HandleFunc("/api/v1/find_mentors/{topic:.+}/{level:[1-5]}", FindMentors).Methods("GET")

	router.PathPrefix("/swagger-ui/").Handler(http.StripPrefix("/swagger-ui/", http.FileServer(http.Dir("./swagger-ui/"))))

	log.Println("Time to serve it up!")
	log.Fatal(http.ListenAndServe(":8080", router))
	log.Println("kthxbye")
}

// Index should return hello world
func Index(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "Hello, %q", html.EscapeString(r.URL.Path))
}

// GetUser swagger:route GET /api/v1/user/{id} Users GetUser
//
// Gets info for a user.
//
// Responses:
//    default: errorResponse
//        200: models.User
func GetUser(w http.ResponseWriter, r *http.Request) {
	var userID models.UserIDParam
	urlVars := mux.Vars(r)
	idString, ok := urlVars["id"]
	if !ok {
		log.Println("Error retrieving user ID from request. urlVars:", urlVars)
		returnHTTPErrorResponse(w, 300, "Error fetching user")
		return
	}

	var err error
	userID.ID, err = strconv.Atoi(idString)
	if err != nil {
		log.Println("Error converting user ID to integer from request. urlVars:", urlVars)
		returnHTTPErrorResponse(w, 305, "Error fetching user")
		return
	}

	row := dbCon.QueryRow(`
    SELECT
      id
      ,username
      ,display_name
      ,email
      ,created
    FROM main.user
    WHERE id = $1
    `, userID.ID)
	user := new(models.User)
	if err := row.Scan(
		&user.ID,
		&user.Username,
		&user.DisplayName,
		&user.Email,
		&user.Created,
	); err != nil {
		if err == sql.ErrNoRows {
			log.Println("Received request for nonexistent user. UserID:", userID)
			returnHTTPErrorResponse(w, 320, "User does not exist")
			return
		}
		log.Println("Error fetching requested user from database. UserID:", userID, "-- Error:", err)
		returnHTTPErrorResponse(w, 310, "Error fetching user")
		return
	}

	if err := json.NewEncoder(w).Encode(user); err != nil {
		log.Println("Error encoding response for requested user. User:", user, "-- Error:", err)
		returnHTTPErrorResponse(w, 340, "Error fetching user")
		return
	}
}

// NewUser swagger:route POST /api/v1/user Users NewUser
//
// Constructs and persists a new user based on the provided parameters
//
// Responses:
//    default: errorResponse
//        200: models.User
func NewUser(w http.ResponseWriter, r *http.Request) {
	decoder := json.NewDecoder(r.Body)
	var u models.NewUserParam
	err := decoder.Decode(&u)
	if err != nil {
		log.Println("Error decoding request body for NewUser. Body:", r.Body, "-- Error:", err)
		returnHTTPErrorResponse(w, 200, "Error creating new user")
		return
	}
	// TODO: Encrypt user password

	tx, err := dbCon.Begin()
	if err != nil {
		log.Println("Error starting new transaction for inserting new channel. Error:", err, "-- User:", u)
		returnHTTPErrorResponse(w, 210, "Error creating new user")
		return
	}
	defer tx.Rollback()

	var userInsertID int
	err = tx.QueryRow(`
    INSERT INTO main.user
    (username, display_name, email, password, created, last_activity)
    VALUES ($1, $2, $3, $4, now(), now())
    returning id;
  `, u.Username, u.DisplayName, u.Email, u.Password).Scan(&userInsertID)
	if err != nil {
		if err.(*pq.Error).Code.Name() == "unique_violation" {
			constraintPattern, _ := regexp.Compile("violates unique constraint \"(.+?)\"")
			matchSlice := constraintPattern.FindSubmatch([]byte(err.(*pq.Error).Message))
			if len(matchSlice) < 2 {
				log.Println("Received request to create user but encountered unknown unique constraint violation. Error:", err)
				returnHTTPErrorResponse(w, 230, "Unknown error attempting to create user")
				return
			}
			constraint := string(matchSlice[1])
			if constraint == "user_unq_username" {
				log.Println("Received request to create user with duplicate username. Username:", u.Username)
				returnHTTPErrorResponse(w, 240, "Username already in use")
				return
			} else if constraint == "user_unq_email" {
				log.Println("Received request to create user with duplicate email. Email:", u.Email)
				returnHTTPErrorResponse(w, 250, "Email already in use")
				return
			}
		}
		log.Println("Error inserting new user. Error:", err, "-- User:", u)
		returnHTTPErrorResponse(w, 260, "Error creating new user")
		return
	}

	err = tx.Commit()
	if err != nil {
		log.Println("Error committing transaction for new user. Error:", err, "-- User:", u)
		returnHTTPErrorResponse(w, 280, "Error creating new user")
		return
	}
	log.Println("Successfully created new user")
}

// GetTopic swagger:route GET /api/v1/topic/{id} Topics GetTopic
//
// Gets info for a topic.
//
// Responses:
//    default: errorResponse
//        200: models.Topic
func GetTopic(w http.ResponseWriter, r *http.Request) {
	var topicID models.TopicIDParam
	urlVars := mux.Vars(r)
	idString, ok := urlVars["id"]
	if !ok {
		log.Println("Error retrieving topic ID from request. urlVars:", urlVars)
		// TODO: Should we be using similar error numbers or new ones?
		returnHTTPErrorResponse(w, 300, "Error fetching topic")
		return
	}

	var err error
	topicID.ID, err = strconv.Atoi(idString)
	if err != nil {
		log.Println("Error converting topic ID to integer from request. urlVars:", urlVars)
		returnHTTPErrorResponse(w, 305, "Error fetching topic")
		return
	}

	row := dbCon.QueryRow(`
    SELECT
      id
			,name
    FROM main.topic
    WHERE id = $1
    `, topicID.ID)
	topic := new(models.Topic)
	if err := row.Scan(
		&topic.ID,
		&topic.Name,
	); err != nil {
		if err == sql.ErrNoRows {
			log.Println("Received request for nonexistent topic. TopicID:", topicID)
			returnHTTPErrorResponse(w, 320, "Topic does not exist")
			return
		}
		log.Println("Error fetching requested topic from database. TopicID:", topicID, "-- Error:", err)
		returnHTTPErrorResponse(w, 310, "Error fetching topic")
		return
	}

	if err := json.NewEncoder(w).Encode(topic); err != nil {
		log.Println("Error encoding response for requested topic. Topic:", topic, "-- Error:", err)
		returnHTTPErrorResponse(w, 340, "Error fetching topic")
		return
	}
}

// NewTopic swagger:route POST /api/v1/topic Topics NewTopic
//
// Constructs and persists a new Topic based on the provided parameters
//
// Responses:
//    default: errorResponse
//        200: models.Topic
func NewTopic(w http.ResponseWriter, r *http.Request) {
	decoder := json.NewDecoder(r.Body)
	var t models.NewTopicParam
	err := decoder.Decode(&t)
	if err != nil {
		log.Println("Error decoding request body for NewTopic. Body:", r.Body, "-- Error:", err)
		returnHTTPErrorResponse(w, 200, "Error creating new topic")
		return
	}

	tx, err := dbCon.Begin()
	if err != nil {
		log.Println("Error starting new transaction for inserting topic. Error:", err, "-- Topic:", t)
		returnHTTPErrorResponse(w, 210, "Error creating new topic")
		return
	}
	defer tx.Rollback()

	var topicInsertID int
	err = tx.QueryRow(`
    INSERT INTO main.topic
    (name)
    VALUES ($1)
    returning id;
  `, t.Name).Scan(&topicInsertID)
	if err != nil {
		if err.(*pq.Error).Code.Name() == "unique_violation" {
			constraintPattern, _ := regexp.Compile("violates unique constraint \"(.+?)\"")
			matchSlice := constraintPattern.FindSubmatch([]byte(err.(*pq.Error).Message))
			if len(matchSlice) < 2 {
				log.Println("Received request to create topic but encountered unknown unique constraint violation. Error:", err)
				returnHTTPErrorResponse(w, 230, "Unknown error attempting to create topic")
				return
			}
			constraint := string(matchSlice[1])
			if constraint == "topic_unq_name" {
				log.Println("Received request to create topic with duplicate name. Name:", t.Name)
				returnHTTPErrorResponse(w, 240, "Topic name already in use")
				return
			}
			// TODO: Is there a better way to organize this?
		}
		log.Println("Error inserting new topic. Error:", err, "-- Topic:", t)
		returnHTTPErrorResponse(w, 260, "Error creating new topic")
		return
	}

	err = tx.Commit()
	if err != nil {
		log.Println("Error committing transaction for new topic. Error:", err, "-- Topic:", t)
		returnHTTPErrorResponse(w, 280, "Error creating new topic")
		return
	}
	log.Println("Successfully created new topic")
}

// GetTopicsLike swagger:route GET /api/v1/topic_like/{part} Topics GetTopicsLike
//
// Gets topics similar to the given string
//
// Responses:
//    default: errorResponse
//        200: models.Topic
func GetTopicsLike(w http.ResponseWriter, r *http.Request) {
	urlVars := mux.Vars(r)
	partString, ok := urlVars["part"]
	if !ok {
		log.Println("Error retrieving topic part from request. urlVars:", urlVars)
		returnHTTPErrorResponse(w, 300, "Error retrieving similar topic")
		return
	}

	topicSlice := make([]*models.Topic, 0)
	rows, err := dbCon.Query(`
    SELECT
      id
			,name
    FROM main.topic
    WHERE name LIKE '%' || $1 || '%'
    `, partString)
	if err != nil {
		log.Println("Error retrieving similar topics from database. TopicPart:", partString, " -- Error:", err)
		returnHTTPErrorResponse(w, 320, "Error retrieving similar topic")
		return
	}
	for rows.Next() {
		topic := new(models.Topic)
		if err := rows.Scan(
			&topic.ID,
			&topic.Name,
		); err != nil {
			log.Println("Error scanning similar topic from database. TopicPart:", partString, "-- Error:", err)
			returnHTTPErrorResponse(w, 310, "Error retrieving similar topic")
			continue
		}
		topicSlice = append(topicSlice, topic)
	}

	if err := json.NewEncoder(w).Encode(topicSlice); err != nil {
		log.Println("Error encoding response for requested topics. TopicSlice:", topicSlice, "-- Error:", err)
		returnHTTPErrorResponse(w, 340, "Error retrieving similar topic")
		return
	}

	// TODO: Do fancy fuzzy-matching stuff
}

// NewMentorTopic swagger:route POST /api/v1/mentor_topic Mentors NewMentorTopic
//
// Constructs and persists a new MentorTopic based on the provided parameters
//
// Responses:
//    default: errorResponse
//        200: models.MentorTopic
func NewMentorTopic(w http.ResponseWriter, r *http.Request) {
	decoder := json.NewDecoder(r.Body)
	var mt models.MentorTopic
	err := decoder.Decode(&mt)
	if err != nil {
		log.Println("Error decoding request body for new MentorTopic. Body:", r.Body, "-- Error:", err)
		returnHTTPErrorResponse(w, 200, "Error creating new mentor topic")
		return
	}

	tx, err := dbCon.Begin()
	if err != nil {
		log.Println("Error starting new transaction for inserting mentor topic. Error:", err, "-- MentorTopic:", mt)
		returnHTTPErrorResponse(w, 210, "Error creating new mentor topic")
		return
	}
	defer tx.Rollback()

	_, err = tx.Exec(`
    INSERT INTO main.mentor_topic
    (
			user_id
			,topic_id
			,level
			,description
		)
    VALUES ($1, $2, $3, $4)
  `, mt.UserID, mt.TopicID, mt.Level, mt.Description)
	if err != nil {
		if err.(*pq.Error).Code.Name() == "unique_violation" {
			constraintPattern, _ := regexp.Compile("violates unique constraint \"(.+?)\"")
			matchSlice := constraintPattern.FindSubmatch([]byte(err.(*pq.Error).Message))
			if len(matchSlice) < 2 {
				log.Println("Received request to create mentor topic but encountered unknown unique constraint violation. Error:", err)
				returnHTTPErrorResponse(w, 230, "Unknown error attempting to create mentor topic")
				return
			}
			constraint := string(matchSlice[1])
			if constraint == "mentor_topic_id" {
				log.Println("Received request to create mentor topic with duplicate user and topic IDs. UserID:", mt.UserID, "TopicID:", mt.TopicID)
				returnHTTPErrorResponse(w, 240, "Mentor topic combination already in use")
				return
			}
			// TODO: Is there a better way to organize this?
		}
		log.Println("Error inserting new mentor topic. Error:", err, "-- MentorTopic:", mt)
		returnHTTPErrorResponse(w, 260, "Error creating new mentor topic")
		return
	}

	err = tx.Commit()
	if err != nil {
		log.Println("Error committing transaction for new mentor topic. Error:", err, "-- MentorTopic:", mt)
		returnHTTPErrorResponse(w, 280, "Error creating new mentor topic")
		return
	}
	log.Println("Successfully created new mentor topic")
}

// FindMentors swagger:route GET /api/v1/find_mentors/{topic}/{level} Mentors FindMentors
//
// Returns a list of mentors that are listed for the given topic at or above the given level
//
// Responses:
//    default: errorResponse
//        200: []models.User
func FindMentors(w http.ResponseWriter, r *http.Request) {
	urlVars := mux.Vars(r)
	topicName, ok := urlVars["topic"]
	if !ok {
		log.Println("Error retrieving topic name from request. urlVars:", urlVars)
		returnHTTPErrorResponse(w, 300, "Error retrieving mentors")
		return
	}
	level, err := strconv.Atoi(urlVars["level"])
	if err != nil {
		log.Println("Error retrieving level from request. urlVars:", urlVars, "Error:", err)
		returnHTTPErrorResponse(w, 300, "Error retrieving mentors")
		return
	}

	userSlice := make([]*models.User, 0)
	rows, err := dbCon.Query(`
	SELECT
		u.id
		,u.username
		,u.display_name
		,u.created
		,u.last_activity
		,u.description
		,u.icon_url
	FROM main.mentor_topic mt
	JOIN main.user u
	ON mt.user_id = u.id
	JOIN main.topic t
	ON mt.topic_id = t.id
	WHERE t.name = $1
	AND mt.level >= $2
	`, topicName, level)
	if err != nil {
		log.Println("Error retrieving mentors from database. TopicName:", topicName, "Level:", level, "-- Error:", err)
		returnHTTPErrorResponse(w, 320, "Error retrieving mentors")
		return
	}
	for rows.Next() {
		user := new(models.User)
		if err := rows.Scan(
			&user.ID,
			&user.Username,
			&user.DisplayName,
			&user.Created,
			&user.LastActivity,
			&user.Description,
			&user.IconURL,
		); err != nil {
			log.Println("Error scanning mentor user from database. TopicName:", topicName, "Level:", level, "-- Error:", err)
			returnHTTPErrorResponse(w, 310, "Error retrieving mentors")
			continue
		}
		userSlice = append(userSlice, user)
	}

	if err := json.NewEncoder(w).Encode(userSlice); err != nil {
		log.Println("Error encoding response for requested topics. UsersSlice:", userSlice, "-- Error:", err)
		returnHTTPErrorResponse(w, 340, "Error retrieving similar topic")
		return
	}
}

func returnHTTPErrorResponse(w http.ResponseWriter, code int, message string) {
	r := &errorResponse{ErrorCode: code, ErrorMessage: message}
	if err := json.NewEncoder(w).Encode(r); err != nil {
		log.Println("Failed to write HTTP Error Response. Error:", err)
	}
}
