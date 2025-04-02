package shanepark.foodbox.api.service;

import jakarta.annotation.PostConstruct;
import lombok.RequiredArgsConstructor;
import lombok.extern.slf4j.Slf4j;
import org.springframework.stereotype.Service;
import org.springframework.util.DigestUtils;
import shanepark.foodbox.api.domain.Menu;
import shanepark.foodbox.api.domain.MenuResponse;
import shanepark.foodbox.api.exception.MenuNotUploadedException;
import shanepark.foodbox.api.repository.MenuRepository;
import shanepark.foodbox.crawl.CrawlConfig;
import shanepark.foodbox.crawl.MenuCrawler;
import shanepark.foodbox.image.domain.ParsedMenu;
import shanepark.foodbox.image.ocr.clova.ImageParserClova;
import shanepark.foodbox.image.ocr.tesseract.ImageParserTesseract;

import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.time.LocalDate;
import java.util.ArrayList;
import java.util.Comparator;
import java.util.List;
import java.util.Objects;
import java.util.stream.Collectors;

@Service
@RequiredArgsConstructor
@Slf4j
public class MenuService {

    private final MenuRepository menuRepository;
    private final MenuCrawler menuCrawler;
    private final ImageParserTesseract imageParserTesseract;
    private final ImageParserClova imageParserClova;
    private final CrawlConfig crawlConfig;
    private String lastImageHash;

    @PostConstruct
    public void init() {
        Boolean isUpToDate = menuRepository.findAll()
                .stream()
                .map(Menu::getDate)
                .max(Comparator.naturalOrder())
                .map(latest -> latest.isAfter(LocalDate.now()))
                .orElse(false);

        if (!isUpToDate) {
            crawl();
        }
    }

    public MenuResponse getTodayMenu(LocalDate today) {
        int dayOfWeek = today.getDayOfWeek().getValue();
        if (dayOfWeek > 5) {
            Menu menu = new Menu(today, List.of("주말에는 도시락이 없습니다."));
            return MenuResponse.of(menu);
        }
        Menu menu = menuRepository.findByDate(today)
                .orElseGet(() -> {
                    crawl();
                    return menuRepository.findByDate(today).orElseThrow(MenuNotUploadedException::new);
                });
        return MenuResponse.of(menu);
    }

    public List<MenuResponse> findAll() {
        return menuRepository.findAll()
                .stream()
                .map(MenuResponse::of)
                .collect(Collectors.toList());
    }

    public synchronized void crawl() {
        long start = System.currentTimeMillis();
        log.info("Start crawling menu");
        Path image = menuCrawler.getImage(crawlConfig);

        try {
            String imageHash = DigestUtils.md5DigestAsHex(Files.readAllBytes(image));
            if (Objects.equals(imageHash, lastImageHash)) {
                log.info("The Image has already parsed before. Skip this crawling (upcoming menu may not be uploaded yet)");
                return;
            }
            lastImageHash = imageHash;
        } catch (IOException e) {
            throw new RuntimeException(e);
        }

        List<ParsedMenu> parsed = new ArrayList<>();
        try {
            parsed = imageParserClova.parse(image);
        } catch (Exception e) {
            log.error("Failed to parse image with Clova", e);
            try {
                parsed = imageParserTesseract.parse(image);
            } catch (Exception e_) {
                log.error("Failed to parse image with Tesseract", e_);
            }
        }

        List<Menu> menus = parsed.stream().map(ParsedMenu::toMenuResponse).toList();
        menuRepository.saveAll(menus);
        log.info("Crawling done. total time taken: {} ms  , : {}", System.currentTimeMillis() - start, parsed);
    }

}
