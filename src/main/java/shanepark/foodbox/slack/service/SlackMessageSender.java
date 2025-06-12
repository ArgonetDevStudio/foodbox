package shanepark.foodbox.slack.service;

import com.google.gson.Gson;
import lombok.extern.slf4j.Slf4j;
import org.springframework.stereotype.Component;
import shanepark.foodbox.slack.domain.dto.SlackPayload;

import java.io.IOException;
import java.net.URI;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;

@Component
@Slf4j
public class SlackMessageSender {

    static private final Gson gson = new Gson();

    public int sendMessage(String slackUrl, String slackToken, SlackPayload payload) throws IOException, InterruptedException {
        try (HttpClient client = HttpClient.newHttpClient()) {
            HttpResponse<String> response = client.send(HttpRequest.newBuilder()
                    .uri(URI.create(slackUrl + slackToken))
                    .header("Content-Type", "application/json")
                    .POST(HttpRequest.BodyPublishers.ofString(gson.toJson(payload)))
                    .build(), HttpResponse.BodyHandlers.ofString());
            int statusCode = response.statusCode();
            if (statusCode != 200) {
                log.info("Failed to send message to slack. payload={}, statusCode = {}, response = {}, ", payload, statusCode, response);
            }
            return statusCode;
        }
    }

}
