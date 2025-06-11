package shanepark.foodbox.integration;

import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Test;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import shanepark.foodbox.crawl.CrawlConfig;
import shanepark.foodbox.crawl.MenuCrawler;
import shanepark.foodbox.image.domain.ParsedMenu;
import shanepark.foodbox.image.ocr.ImageMarginCalculatorDaejeon;
import shanepark.foodbox.image.ocr.clova.ImageParserClova;
import shanepark.foodbox.image.ocr.clova.NaverClovaApi;
import shanepark.foodbox.image.ocr.clova.NaverClovaConfig;

import java.io.IOException;
import java.nio.file.Path;
import java.time.Clock;
import java.util.List;

import static shanepark.foodbox.integration.TestConstant.*;

public class IntegrationTest {
    private final Logger logger = LoggerFactory.getLogger(IntegrationTest.class);

    /**
     * Never commit the secret key to the repository.
     */
    @Test
    @DisplayName("If you want to run this test, you need to set the url and secretKey in the clovaApiTest method.")
    public void clovaApiTest() throws IOException {
        String url = "";
        String secretKey = "";

        if (url.isEmpty() || secretKey.isEmpty()) {
            logger.info("Skip integration test");
            return;
        }

        NaverClovaConfig config = new NaverClovaConfig(url, secretKey);
        NaverClovaApi api = new NaverClovaApi(config);
        ImageParserClova parser = new ImageParserClova(new ImageMarginCalculatorDaejeon(), api, Clock.systemDefaultZone());

        List<ParsedMenu> result = parser.parse(crawlImage());
        for (ParsedMenu parsedMenu : result) {
            logger.info("parsedMenu: {}", parsedMenu);
        }
    }

    private Path crawlImage() {
        MenuCrawler crawler = new MenuCrawler();
        CrawlConfig crawlConfig = new CrawlConfig(CRAWL_URL, CRAWL_CSS_SELECTOR, CRAWL_IMAGE_INDEX);
        return crawler.getImage(crawlConfig);
    }

}
