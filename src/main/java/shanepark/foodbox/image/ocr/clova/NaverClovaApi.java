package shanepark.foodbox.image.ocr.clova;

import com.google.gson.JsonArray;
import com.google.gson.JsonObject;
import lombok.RequiredArgsConstructor;
import org.springframework.boot.web.client.RestTemplateBuilder;
import org.springframework.http.HttpEntity;
import org.springframework.http.HttpHeaders;
import org.springframework.http.ResponseEntity;
import org.springframework.stereotype.Component;
import org.springframework.web.client.RestTemplate;

import java.time.Duration;
import java.time.Instant;

import static org.springframework.http.HttpMethod.POST;

@Component
@RequiredArgsConstructor
public class NaverClovaApi {

    private final NaverClovaConfig naverClovaConfig;
    private final RestTemplate restTemplate = new RestTemplateBuilder()
            .setConnectTimeout(Duration.ofSeconds(5))
            .setReadTimeout(Duration.ofSeconds(10))
            .build();

    public String clovaRequest(String base64Image) {
        String requestBody = createRequestBody(base64Image);
        HttpHeaders headers = createHeaders();
        HttpEntity<String> request = new HttpEntity<>(requestBody, headers);
        ResponseEntity<String> response = restTemplate.exchange(naverClovaConfig.getUrl(), POST, request, String.class);
        return response.getBody();
    }

    private static String createRequestBody(String base64Image) {
        JsonObject bodyJson = new JsonObject();
        bodyJson.add("images", createImageArray(base64Image));
        bodyJson.addProperty("lang", "ko");
        bodyJson.addProperty("requestId", "string");
        bodyJson.addProperty("resultType", "string");
        bodyJson.addProperty("timestamp", Instant.now().toEpochMilli());
        bodyJson.addProperty("version", "V1");
        return bodyJson.toString();
    }

    private static JsonArray createImageArray(String base64Image) {
        JsonArray images = new JsonArray();
        JsonObject image = new JsonObject();
        image.addProperty("format", "png");
        image.addProperty("name", "menu");
        image.addProperty("data", base64Image);
        images.add(image);
        return images;
    }


    private HttpHeaders createHeaders() {
        HttpHeaders headers = new HttpHeaders();
        headers.set("X-OCR-SECRET", naverClovaConfig.getSecretKey());
        headers.set("Content-Type", "application/json");
        return headers;
    }

}
