FROM eclipse-temurin:25-jre-jammy

WORKDIR /app

COPY /build/libs/foodbox-0.0.1-SNAPSHOT.jar /app/foodbox.jar

EXPOSE 80

ENTRYPOINT ["java", "-jar", "/app/foodbox.jar"]
