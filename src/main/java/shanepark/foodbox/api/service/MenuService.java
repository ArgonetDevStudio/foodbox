package shanepark.foodbox.api.service;

import jakarta.annotation.PostConstruct;
import lombok.RequiredArgsConstructor;
import lombok.extern.slf4j.Slf4j;
import org.springframework.stereotype.Service;
import shanepark.foodbox.api.domain.MenuResponse;
import shanepark.foodbox.api.exception.MenuNotUploadedException;
import shanepark.foodbox.api.repository.MenuRepository;
import shanepark.foodbox.crawl.CrawlConfig;
import shanepark.foodbox.crawl.MenuCrawler;
import shanepark.foodbox.image.domain.ParsedMenu;
import shanepark.foodbox.image.service.ImageParser;
import shanepark.foodbox.image.service.ImageParserV2;
import shanepark.foodbox.ocr.OCRConfig;

import java.io.InputStream;
import java.time.LocalDate;
import java.util.List;

@Service
@RequiredArgsConstructor
@Slf4j
public class MenuService {

    private final MenuRepository menuRepository;
    private final MenuCrawler menuCrawler;
//    private final ImageParser imageParser;
    private final ImageParserV2 imageParser;
    private final CrawlConfig crawlConfig;
    private final OCRConfig ocrConfig;

    @PostConstruct
    public void init() {
        crawl();
    }

    public MenuResponse getTodayMenu() {
        LocalDate today = LocalDate.now();
        int dayOfWeek = today.getDayOfWeek().getValue();
        if (dayOfWeek > 5) {
            return new MenuResponse(today, List.of("주말에는 도시락이 없습니다."));
        }
        return menuRepository.findByDate(today)
                .orElseGet(() -> {
                    crawl();
                    return menuRepository.findByDate(today).orElseThrow(MenuNotUploadedException::new);
                });
    }

    public List<MenuResponse> findAll() {
        getTodayMenu(); // Ensure today's menu is up-to-date
        return menuRepository.findAll();
    }

    public void crawl() {
        long start = System.currentTimeMillis();
        log.info("Start crawling menu");
        InputStream inputStream = menuCrawler.getImage(crawlConfig);
//        List<ParsedMenu> parsed = imageParser.parse(inputStream);
        List<ParsedMenu> parsed = imageParser.parse(inputStream,ocrConfig);
        for (ParsedMenu menu : parsed) {
            MenuResponse resp = menu.toMenuResponse();
            menuRepository.save(resp);
        }
        log.info("Crawling done. total time taken: {} ms  , : {}", System.currentTimeMillis() - start, parsed);
    }

}
