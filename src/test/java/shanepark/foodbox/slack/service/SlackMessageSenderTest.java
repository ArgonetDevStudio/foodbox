package shanepark.foodbox.slack.service;

import org.junit.jupiter.api.Test;
import shanepark.foodbox.slack.domain.dto.SlackPayload;

import java.io.IOException;

import static org.assertj.core.api.Assertions.assertThat;

class SlackMessageSenderTest {

    @Test
    void sendMessageTest() throws IOException, InterruptedException {
        // Given
        String slackToken = "SLACK_TOKEN_HERE_FOR_TEST";
        String channel = "#foodbox";

        // When
        SlackPayload slackPayload = new SlackPayload(channel, "점심봇", "점심메뉴안내1", ":bento:");
        SlackMessageSender slackMessageSender = new SlackMessageSender();
        int status = slackMessageSender.sendMessage("https://hooks.slack.com/services", slackToken, slackPayload);

        // Then
        // to make it success(200), you need to change slack token and hannel
        assertThat(status).isEqualTo(302);
    }

}
