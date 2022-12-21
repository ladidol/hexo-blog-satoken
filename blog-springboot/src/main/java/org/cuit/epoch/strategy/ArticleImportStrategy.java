package org.cuit.epoch.strategy;

import org.springframework.web.multipart.MultipartFile;

/**
 * @author: Xiaoqiang-Ladidol
 * @date: 2022/12/21 18:37
 * @description: {文章导入策略}
 */
public interface ArticleImportStrategy {

    /**
     * 导入文章
     *
     * @param file 文件
     */
    void importArticles(MultipartFile file);
}
