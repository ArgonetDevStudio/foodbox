package shanepark.foodbox.api.service;

import jakarta.annotation.PostConstruct;
import lombok.RequiredArgsConstructor;
import lombok.extern.slf4j.Slf4j;
import org.springframework.stereotype.Service;
import shanepark.foodbox.api.domain.Menu;
import shanepark.foodbox.api.domain.MenuResponse;
import shanepark.foodbox.api.exception.MenuNotUploadedException;
import shanepark.foodbox.api.repository.MenuRepository;
import shanepark.foodbox.crawl.CrawlConfig;
import shanepark.foodbox.crawl.MenuCrawler;

import java.time.LocalDate;
import java.util.Comparator;
import java.util.List;
import java.util.stream.Collectors;

@Service
@RequiredArgsConstructor
@Slf4j
public class MenuService {

    private final MenuRepository menuRepository;
    private final MenuCrawler menuCrawler;
    private final CrawlConfig crawlConfig;

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
        
        List<Menu> menus = menuCrawler.crawlMenus(crawlConfig);
        log.info("Saving {} menus", menus.size());
        menuRepository.saveAll(menus);
        
        log.info("Crawling done. total time taken: {} ms", System.currentTimeMillis() - start);
    }

}
