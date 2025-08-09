package shanepark.foodbox.crawl;

import lombok.RequiredArgsConstructor;
import lombok.extern.slf4j.Slf4j;
import org.jsoup.Jsoup;
import org.jsoup.nodes.Document;
import org.jsoup.nodes.Element;
import org.jsoup.select.Elements;
import org.springframework.stereotype.Component;
import shanepark.foodbox.api.domain.Menu;
import shanepark.foodbox.api.exception.ImageCrawlException;

import java.io.IOException;
import java.time.LocalDate;
import java.util.ArrayList;
import java.util.List;
import java.util.Optional;

@Component
@Slf4j
@RequiredArgsConstructor
public class MenuCrawler {

    public List<Menu> crawlMenus(CrawlConfig crawlConfig) {
        try {
            String url = crawlConfig.getCrawlUrl();
            log.info("Crawling menus from: {}", url);

            Document document = Jsoup.connect(url).get();
            return extractMenusFromDocument(document);
        } catch (IOException e) {
            throw new ImageCrawlException(e);
        }
    }

    private List<Menu> extractMenusFromDocument(Document document) {
        List<Menu> menus = new ArrayList<>();
        Optional<DateInfo> dateInfo = extractDateInfo(document);

        if (dateInfo.isEmpty()) {
            log.warn("Could not extract date information from document");
            return menus;
        }

        Elements tableCells = document.select("td.info");
        for (Element cell : tableCells) {
            extractMenuFromCell(cell, dateInfo.get()).ifPresent(menus::add);
        }

        log.info("Successfully extracted {} menus", menus.size());
        return menus;
    }

    private Optional<DateInfo> extractDateInfo(Document document) {
        Element monthHeader = document.selectFirst("div.hgroup strong");
        if (monthHeader == null) {
            return Optional.empty();
        }

        String monthText = monthHeader.text();
        String[] parts = monthText.split("[년월\\s]+");

        if (parts.length < 2) {
            log.warn("Invalid date format in header: {}", monthText);
            return Optional.empty();
        }

        try {
            int year = Integer.parseInt(parts[0]);
            int month = Integer.parseInt(parts[1]);
            return Optional.of(new DateInfo(year, month));
        } catch (NumberFormatException e) {
            log.warn("Failed to parse date from header: {}", monthText, e);
            return Optional.empty();
        }
    }

    private Optional<Menu> extractMenuFromCell(Element cell, DateInfo dateInfo) {
        Optional<Integer> dayOpt = extractDay(cell);
        if (dayOpt.isEmpty()) {
            return Optional.empty();
        }

        LocalDate date = LocalDate.of(dateInfo.year(), dateInfo.month(), dayOpt.get());
        List<String> menuItems = extractMenuItems(cell);

        Menu menu = new Menu(date, menuItems);
        return Optional.of(menu);
    }

    private Optional<Integer> extractDay(Element cell) {
        Element dayElement = cell.selectFirst("span.dayy");
        if (dayElement == null) {
            return Optional.empty();
        }

        String dayText = dayElement.text().replace("일", "");
        try {
            return Optional.of(Integer.parseInt(dayText));
        } catch (NumberFormatException e) {
            log.warn("Failed to parse day from text: {}", dayText, e);
            return Optional.empty();
        }
    }

    private List<String> extractMenuItems(Element cell) {
        List<String> menuItems = new ArrayList<>();
        Elements menuContainers = cell.select("li");

        for (Element li : menuContainers) {
            Elements pTags = li.select("p");

            if (!pTags.isEmpty()) {
                for (Element p : pTags) {
                    String menuItem = p.text().trim();
                    if (!menuItem.isEmpty()) {
                        menuItems.add(menuItem);
                    }
                }
                continue;
            }
            String menuItem = li.text().trim();
            if (!menuItem.isEmpty()) {
                menuItems.add(menuItem);
            }
        }
        return menuItems;
    }

    private record DateInfo(int year, int month) {
    }
}
