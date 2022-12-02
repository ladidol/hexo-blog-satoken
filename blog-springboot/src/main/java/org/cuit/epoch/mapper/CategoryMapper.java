package org.cuit.epoch.mapper;

import com.baomidou.mybatisplus.core.mapper.BaseMapper;
import org.apache.ibatis.annotations.Param;
import org.cuit.epoch.dto.category.CategoryBackDTO;
import org.cuit.epoch.dto.category.CategoryDTO;
import org.cuit.epoch.entity.Category;
import org.cuit.epoch.vo.ConditionVO;
import org.springframework.stereotype.Repository;

import java.util.List;

/**
 * @author: ladidol
 * @date: 2022/11/16 19:24
 * @description:
 */
@Repository
public interface CategoryMapper extends BaseMapper<Category> {

    /**
     * 查询分类和对应文章数量
     *
     * @return 分类列表
     */
    List<CategoryDTO> listCategoryDTO();

    /**
     * 查询后台分类列表
     *
     * @param current   页码
     * @param size      大小
     * @param condition 条件
     * @return {@link List< CategoryBackDTO >} 分类列表
     */
    List<CategoryBackDTO> listCategoryBackDTO(@Param("current") Long current, @Param("size") Long size, @Param("condition") ConditionVO condition);

}