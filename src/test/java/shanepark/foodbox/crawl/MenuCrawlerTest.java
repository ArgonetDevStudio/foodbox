package shanepark.foodbox.crawl;

import org.junit.jupiter.api.Test;

import java.io.IOException;
import java.io.InputStream;

import static org.assertj.core.api.Assertions.assertThat;

class MenuCrawlerTest {

    private final MenuCrawler menuCrawler = new MenuCrawler();

    @Test
    void getImage() throws IOException {
        // Given
        String url = "https://daejeonyuseong.modoo.at/?link=8dpmx8ms";
        String cssSelector = ".gallery_img img";

        // When
        InputStream inputStream = menuCrawler.getImage(new CrawlConfig(url, cssSelector, 0));

        // Then
        assertThat(inputStream).isNotNull();
        assertThat(inputStream.available()).isGreaterThan(0);
        assertThat(inputStream.read()).isNotEqualTo(-1);
    }

}
