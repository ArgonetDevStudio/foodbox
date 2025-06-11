package shanepark.foodbox.crawl;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import lombok.RequiredArgsConstructor;
import lombok.extern.slf4j.Slf4j;
import org.jsoup.Jsoup;
import org.jsoup.nodes.Document;
import org.jsoup.nodes.Element;
import org.springframework.stereotype.Component;
import shanepark.foodbox.api.exception.ImageCrawlException;

import java.io.BufferedInputStream;
import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;

import static java.nio.file.StandardCopyOption.REPLACE_EXISTING;

@Component
@Slf4j
@RequiredArgsConstructor
public class MenuCrawler {
    private final ObjectMapper objectMapper;

    public Path getImage(CrawlConfig crawlConfig) {
        try {
            String url = crawlConfig.getCrawlUrl();

            Document document = Jsoup.connect(url).get();
            String cssSelector = crawlConfig.getCssSelector();
            Element scriptElement = document.selectFirst(cssSelector);
            if (scriptElement == null) {
                throw new IllegalArgumentException("No script element found with selector: " + cssSelector);
            }

            String json = scriptElement.html();
            JsonNode jsonNode = objectMapper.readTree(json);
            JsonNode sessions = jsonNode.at(crawlConfig.getCrawlImageExpr());
            String imageSrc = null;
            for (JsonNode session : sessions) {
                if ("Image".equals(session.get("type").asText())) {
                    JsonNode attachments = session.get("attachments");
                    JsonNode image = attachments.get(crawlConfig.getImageIndex());
                    imageSrc = image.get("imgOriginUrl").asText();
                }
            }

            if (imageSrc == null) {
                throw new IllegalArgumentException("No image source found in the JSON data");
            }
            log.info("imageSrc: {}", imageSrc);

            try (BufferedInputStream bufferedInputStream = Jsoup.connect(imageSrc)
                    .ignoreContentType(true)
                    .execute().bodyStream()) {
                Path tempFile = Files.createTempFile("temp", ".png");
                Files.copy(bufferedInputStream, tempFile, REPLACE_EXISTING);
                return tempFile;
            }
        } catch (IOException e) {
            throw new ImageCrawlException(e);
        }
    }

}
