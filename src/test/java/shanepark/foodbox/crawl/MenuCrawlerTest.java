package shanepark.foodbox.crawl;

import com.fasterxml.jackson.databind.ObjectMapper;
import org.junit.jupiter.api.Test;

import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;

import static org.assertj.core.api.Assertions.assertThat;
import static shanepark.foodbox.integration.TestConstant.*;

class MenuCrawlerTest {

    private final MenuCrawler menuCrawler = new MenuCrawler(new ObjectMapper());

    @Test
    void getImage() throws IOException {
        // When
        Path path = menuCrawler.getImage(new CrawlConfig(CRAWL_URL, CRAWL_CSS_SELECTOR, CRAWL_IMAGE_EXPR, CRAWL_IMAGE_INDEX));

        // Then
        assertThat(path).isNotNull();
        assertThat(Files.size(path)).isGreaterThan(0);
    }

}
