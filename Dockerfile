# Use an official OpenJDK runtime as a parent image
FROM openjdk:21-jdk-slim

WORKDIR /app

# Copy the Spring Boot application JAR file into the container
COPY /build/libs/foodbox-0.0.1-SNAPSHOT.jar /app/foodbox.jar

# Expose the port that the application will run on
EXPOSE 80

# Run the Spring Boot application
ENTRYPOINT ["java", "-jar", "/app/foodbox.jar"]
