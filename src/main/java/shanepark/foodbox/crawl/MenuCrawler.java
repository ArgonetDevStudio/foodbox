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

import java.io.BufferedInputStream;
import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.time.LocalDate;
import java.time.YearMonth;
import java.time.format.DateTimeFormatter;
import java.util.ArrayList;
import java.util.List;
import java.util.Optional;

import static java.nio.file.StandardCopyOption.REPLACE_EXISTING;

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

    /**
     * Eisodosirak 웹사이트에서 메뉴 이미지를 크롤링합니다.
     *
     * @param crawlConfig 크롤링 설정
     * @return 다운로드된 이미지 파일 경로
     */
    public Path getMenuImage(CrawlConfig crawlConfig) {
        try {
            String listUrl = crawlConfig.getCrawlUrl();
            log.info("Crawling menu image from: {}", listUrl);

            // 1. 게시판 목록 페이지에서 최신 식단표 링크 찾기
            Document listDocument = Jsoup.connect(listUrl).get();
            Elements links = listDocument.select(".bbs-list a.aline");

            if (links.isEmpty()) {
                throw new ImageCrawlException("No menu links found on the page");
            }

            // 현재 년월로 검색 (예: "2025년 10월")
            YearMonth currentMonth = YearMonth.now();
            String searchPattern = currentMonth.format(DateTimeFormatter.ofPattern("yyyy년 MM월"));

            String menuDetailUrl = null;
            for (Element link : links) {
                String linkText = link.text();
                if (linkText.contains(searchPattern) && linkText.contains("식단표")) {
                    menuDetailUrl = link.attr("href");
                    log.info("Found menu link: {} -> {}", linkText, menuDetailUrl);
                    break;
                }
            }

            if (menuDetailUrl == null) {
                throw new ImageCrawlException("No menu link found for current month: " + searchPattern);
            }

            // 2. 상세 페이지에서 이미지 URL 찾기
            Document detailDocument = Jsoup.connect(menuDetailUrl).get();
            Element contentDiv = detailDocument.selectFirst("#bo_v_con");

            if (contentDiv == null) {
                throw new ImageCrawlException("No content div found (#bo_v_con)");
            }

            Element imgElement = contentDiv.selectFirst("img");
            if (imgElement == null) {
                // a 태그로 감싸진 img를 찾아봄
                Element linkElement = contentDiv.selectFirst("a");
                if (linkElement != null) {
                    String imageUrl = linkElement.attr("href");
                    if (imageUrl != null && !imageUrl.isEmpty()) {
                        return downloadImage(imageUrl);
                    }
                }
                throw new ImageCrawlException("No image found in content");
            }

            String imageSrc = imgElement.attr("src");
            log.info("Found image: {}", imageSrc);

            // 3. 이미지 다운로드
            return downloadImage(imageSrc);

        } catch (IOException e) {
            throw new ImageCrawlException(e);
        }
    }

    private Path downloadImage(String imageUrl) throws IOException {
        log.info("Downloading image from: {}", imageUrl);

        try (BufferedInputStream bufferedInputStream = Jsoup.connect(imageUrl)
                .ignoreContentType(true)
                .execute().bodyStream()) {

            Path tempFile = Files.createTempFile("menu", ".jpg");
            Files.copy(bufferedInputStream, tempFile, REPLACE_EXISTING);
            log.info("Image downloaded to: {}", tempFile);
            return tempFile;
        }
    }

    private record DateInfo(int year, int month) {
    }
}
