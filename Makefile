NAME=kinesiscrd

all: run

run: 
	go build -o $(NAME) .
	./$(NAME)